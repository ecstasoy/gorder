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
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

type Consumer struct {
	app app.Application
}

func NewConsumer(app app.Application) *Consumer {
	return &Consumer{
		app: app,
	}
}

func (c *Consumer) Listen(ch *amqp.Channel) {
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
		logrus.Fatal("failed to consume message: %w", err)
	}

	timeoutQ, err := ch.QueueDeclare(broker.EventOrderPaymentTimeout, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare timeout queue: %w", err))
	}
	timeoutMsgs, err := ch.Consume(timeoutQ.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to consume timeout message: %w", err))
	}

	logrus.Infof("Order consumer started, listening on queues: %s, %s", broker.EventOrderPaid, broker.EventOrderPaymentTimeout)

	var forever chan struct{}
	go func() {
		for msg := range msgs {
			c.handleMessage(ch, msg, q)
		}
	}()
	go func() {
		for msg := range timeoutMsgs {
			c.handlePaymentTimeout(ch, msg, timeoutQ)
		}
	}()

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
	o := &domain.Order{ID: paid.ID, CustomerID: paid.CustomerID, Status: paid.Status, PaymentLink: paid.PaymentLink}

	_, err = c.app.Commands.UpdateOrder.Handle(ctx, command.UpdateOrder{
		Order: o,
		UpdateFunc: func(ctx context.Context, oldOrder *domain.Order) (*domain.Order, error) {
			if err := oldOrder.UpdateStatus(o.Status); err != nil {
				return nil, err
			}
			return oldOrder, nil
		},
	})

	if err != nil {
		var conflictErr *domain.StatusConflictError
		if stderrors.As(err, &conflictErr) {
			// 状态冲突：订单已被取消但用户付款成功，发起退款
			logging.Warnf(ctx, nil, "Status conflict for order %s, publishing refund event", o.ID)
			refundErr := broker.PublishEvent(ctx, broker.PublishEventReq{
				Channel:  ch,
				Routing:  broker.Direct,
				Queue:    broker.EventOrderRefund,
				Exchange: "",
				Body: broker.OrderRefundPayload{
					OrderID:         o.ID,
					CustomerID:      o.CustomerID,
					PaymentIntentID: paid.PaymentIntentID,
				},
			})
			if refundErr != nil {
				logging.Errorf(ctx, nil, "Failed to publish refund event for order %s: %v", o.ID, refundErr)
			}
			// 冲突不重试，正常 ack
			return
		}
		logging.Errorf(ctx, nil, "Failed to update order, orderID: %s, error: %v", o.ID, err)
		if retryErr := broker.HandleRetry(ctx, ch, &msg); retryErr != nil {
			logging.Errorf(ctx, nil, "Failed to handle retry message, orderID: %s, error: %v", o.ID, retryErr)
		}
		return
	}

	span.AddEvent("order.updated")
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
