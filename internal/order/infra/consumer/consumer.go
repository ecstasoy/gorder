package consumer

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app"
	"github.com/ecstasoy/gorder/order/app/command"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/pkg/errors"
	amqp "github.com/rabbitmq/amqp091-go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

type Consumer struct {
	app         app.Application
	redisClient *goredis.Client
	publisher   broker.Publisher       // 旧用法,部分路径仍直调
	catalog     *broker.EventCatalog   // ADR-0002 candidate 7:typed publish
}

func NewConsumer(app app.Application, redisClient *goredis.Client, publisher broker.Publisher) *Consumer {
	return &Consumer{
		app:         app,
		redisClient: redisClient,
		publisher:   publisher,
		catalog:     broker.NewEventCatalog(publisher),
	}
}

const (
	// Prefetch / worker 数 (对应 channel 级别)
	orderPaidPrefetch = 100
	orderPaidWorkers  = 10

	// timeout 是低频触发 (到 15min ttl 才触发),少量 worker 够了
	paymentTimeoutWorkers = 5

	// 2026-06 refund hardening: order.refunded 是 Stripe webhook 转发,
	// 低频(只在用户付款 + 取消竞态时触发),少量 worker
	orderRefundedWorkers = 5
)

func (c *Consumer) Listen(ch *amqp.Channel) {
	// Prefetch 让 broker 一次 push 多条消息,多个 worker 才能真正并行消化
	if err := ch.Qos(orderPaidPrefetch, 0, false); err != nil {
		logrus.Fatal(fmt.Errorf("failed to set QoS: %w", err))
	}

	q, err := ch.QueueDeclare(broker.EventOrderPaid, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare queue: %w", err))
	}

	err = ch.QueueBind(q.Name, "", broker.EventOrderPaid, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to bind queue: %w", err))
	}

	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatalf("failed to consume order.paid: %v", err)
	}

	timeoutQ, err := ch.QueueDeclare(broker.EventOrderPaymentTimeout, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare timeout queue: %w", err))
	}
	timeoutMsgs, err := ch.Consume(timeoutQ.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to consume timeout message: %w", err))
	}

	refundedQ, err := ch.QueueDeclare(broker.EventOrderRefunded, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare order.refunded queue: %w", err))
	}
	refundedMsgs, err := ch.Consume(refundedQ.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to consume order.refunded: %w", err))
	}

	logrus.Infof("Order consumer started: order.paid x %d workers, order.payment.timeout x %d workers, order.refunded x %d workers",
		orderPaidWorkers, paymentTimeoutWorkers, orderRefundedWorkers)

	// order.paid worker pool
	for i := 0; i < orderPaidWorkers; i++ {
		go func(workerID int) {
			for msg := range msgs {
				c.handleMessage(ch, msg, q)
			}
		}(i)
	}

	// order.payment.timeout worker pool
	for i := 0; i < paymentTimeoutWorkers; i++ {
		go func(workerID int) {
			for msg := range timeoutMsgs {
				c.handlePaymentTimeout(ch, msg, timeoutQ)
			}
		}(i)
	}

	// order.refunded worker pool — payment 转发 Stripe charge.refunded
	for i := 0; i < orderRefundedWorkers; i++ {
		go func(workerID int) {
			for msg := range refundedMsgs {
				c.handleOrderRefunded(ch, msg, refundedQ)
			}
		}(i)
	}

	var forever chan struct{}
	<-forever
}

// orderPaidMsg 在 domain.Order 基础上携带 PaymentIntentID。
// JSON 字段与 broker.OrderPaidEvent 对齐。
type orderPaidMsg struct {
	ID              string              `json:"ID"`
	CustomerID      string              `json:"CustomerID"`
	Status          orderpb.OrderStatus `json:"Status"`
	PaymentLink     string              `json:"PaymentLink"`
	PaymentIntentID string              `json:"PaymentIntentID"`
}

func (c *Consumer) handleMessage(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	logrus.Infof("Order received paid message: %s from %s", string(msg.Body), msg.Exchange)

	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers), fmt.Sprintf("rabbitmq.%s.consume", q.Name))
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			logging.Warnf(ctx, nil, "Failed to consume message: %v, error: %v", string(msg.Body), err)
			_ = msg.Nack(false, false)
		} else {
			logging.Infof(ctx, nil, "Message consumed successfully: %s", string(msg.Body))
			_ = msg.Ack(false)
		}
	}()

	paid := &orderPaidMsg{}
	if err = json.Unmarshal(msg.Body, paid); err != nil {
		err = errors.Wrap(err, "failed to unmarshal order")
		return
	}

	// dispatch to ConfirmOrder saga。saga 内部:
	//   - 正常路径: Mongo Tx { MarkPaid } + stockGRPC.Confirm
	//   - StatusConflict + Order=CANCELLED: 在**同一个 Mongo tx 内**写
	//     refund 请求到 outbox。worker 异步推到 payment 服务消费触发 Stripe Refund。
	//   PaymentIntentID 是 refund 路径必需; saga 内部用它构造 outbox payload。
	_, err = c.app.Commands.ConfirmOrder.Handle(ctx, command.ConfirmOrder{
		OrderID:         paid.ID,
		CustomerID:      paid.CustomerID,
		PaymentIntentID: paid.PaymentIntentID,
	})

	if err != nil {
		var conflictErr *domain.StatusConflictError
		if stderrors.As(err, &conflictErr) {
			// saga 已在同一 Mongo tx 内把 refund record 写进 outbox。consumer 只需 ack。
			// 与之前 (内联 catalog.PublishOrderRefund) 相比这条路径的关键好处是
			// "决定要退款"和"发出退款指令"现在是原子的 —— RabbitMQ 在那一瞬间
			// 不可达不会导致退款事件丢失。
			logging.Warnf(ctx, nil, "Status conflict for order %s; refund queued to outbox by saga", paid.ID)
			err = nil
			return
		}
		logging.Errorf(ctx, nil, "Failed to confirm order, orderID: %s, error: %v", paid.ID, err)
		if retryErr := broker.HandleRetry(ctx, ch, &msg); retryErr != nil {
			logging.Errorf(ctx, nil, "Failed to handle retry message, orderID: %s, error: %v", paid.ID, retryErr)
		}
		return
	}

	span.AddEvent("order.confirmed")
}

func (c *Consumer) handlePaymentTimeout(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(
		broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers),
		fmt.Sprintf("rabbitmq.%s.consume", q.Name))
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			_ = msg.Nack(false, false)
		} else {
			_ = msg.Ack(false)
		}
	}()

	o := &domain.Order{}
	if err = json.Unmarshal(msg.Body, o); err != nil {
		logrus.WithContext(ctx).Errorf("failed to unmarshal timeout message: %v", err)
		return
	}

	_, err = c.app.Commands.CancelOrder.Handle(ctx, command.CancelOrder{
		OrderID:    o.ID,
		CustomerID: o.CustomerID,
	})
	if err != nil {
		logrus.WithContext(ctx).Errorf("failed to cancel order %s on timeout: %v", o.ID, err)
	}
}

// handleOrderRefunded 消费 payment 服务从 Stripe charge.refunded webhook 转发来
// 的 order.refunded 消息,把 RefundID + RefundedAt 写回 Mongo Order(不改 Status)。
// Order.MarkRefunded() 是幂等的,Stripe 重发或 RabbitMQ retry 都安全。
func (c *Consumer) handleOrderRefunded(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(
		broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers),
		fmt.Sprintf("rabbitmq.%s.consume", q.Name))
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			logging.Warnf(ctx, nil, "Failed to consume order.refunded: %v", err)
			if retryErr := broker.HandleRetry(ctx, ch, &msg); retryErr != nil {
				logging.Errorf(ctx, nil, "Failed to handle retry for order.refunded: %v", retryErr)
			}
		} else {
			_ = msg.Ack(false)
		}
	}()

	payload := &broker.OrderRefundedPayload{}
	if err = json.Unmarshal(msg.Body, payload); err != nil {
		err = errors.Wrap(err, "unmarshal order.refunded")
		return
	}

	_, err = c.app.Commands.MarkRefunded.Handle(ctx, command.MarkRefunded{
		OrderID:    payload.OrderID,
		CustomerID: payload.CustomerID,
		RefundID:   payload.RefundID,
		RefundedAt: payload.RefundedAt,
	})
	if err != nil {
		logging.Errorf(ctx, nil, "MarkRefunded failed for order %s: %v", payload.OrderID, err)
		return
	}
	span.AddEvent("order.refunded.persisted")
}
