package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/payment/app"
	"github.com/ecstasoy/gorder/payment/app/command"
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

const (
	// Prefetch 让 broker 一次 push 多条消息到 client-side buffer,
	// 多个 worker 才能真正并行抢到不同的消息。
	// 过小: worker 挨饿,吞吐上不去
	// 过大: 占 client 内存,高失败率下重试压力大
	orderCreatedPrefetch = 100
	orderRefundPrefetch  = 20

	// Worker 数量: order.created 是 hot path,refund 是冷路径
	orderCreatedWorkers = 20
	orderRefundWorkers  = 5
)

func (c *Consumer) Listen(ch *amqp.Channel) {
	// 设置 prefetch. 注意 ch.Qos 是 channel 级别的,对这个 channel 上的所有 consumer 都生效。
	// 这里选 order.created 的值 (较大) 作为 channel 共享 prefetch,refund 共享同一个预取池.
	if err := ch.Qos(orderCreatedPrefetch, 0, false); err != nil {
		logrus.Fatal(fmt.Errorf("failed to set QoS: %w", err))
	}

	q, err := ch.QueueDeclare(broker.EventOrderCreated, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare queue %s: %w", broker.EventOrderCreated, err))
	}
	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		// 如果 Consume 失败,goroutine 会 block 在 nil channel 上,RabbitMQ 侧看不到订阅。
		// 直接 Fatal,让运维立刻知道 consumer 没起来。
		logrus.Fatal(fmt.Errorf("failed to consume queue %s: %w", q.Name, err))
	}

	refundQ, err := ch.QueueDeclare(broker.EventOrderRefund, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare refund queue: %w", err))
	}
	refundMsgs, err := ch.Consume(refundQ.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to consume refund queue: %w", err))
	}

	// 订阅 channel 关闭事件,channel 断开时立刻 Fatal,避免静默变僵尸
	chClose := make(chan *amqp.Error, 1)
	ch.NotifyClose(chClose)
	go func() {
		if chErr := <-chClose; chErr != nil {
			logrus.Fatalf("payment consumer channel closed: %v", chErr)
		}
	}()

	logrus.Infof("Payment consumer started: order.created x %d workers, order.refund x %d workers",
		orderCreatedWorkers, orderRefundWorkers)

	// order.created worker pool
	// 多个 goroutine 同时 range 同一个 channel, Go 保证每条消息只被一个 goroutine 拿到
	for i := 0; i < orderCreatedWorkers; i++ {
		go func(workerID int) {
			for msg := range msgs {
				c.handleMessage(ch, msg, q)
			}
		}(i)
	}

	// order.refund worker pool
	for i := 0; i < orderRefundWorkers; i++ {
		go func(workerID int) {
			for msg := range refundMsgs {
				c.handleRefund(ch, msg, refundQ)
			}
		}(i)
	}

	var forever chan struct{}
	<-forever
}

func (c *Consumer) handleRefund(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers), fmt.Sprintf("rabbitmq.%s.consume", q.Name))
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			logging.Warnf(ctx, nil, "Failed to process refund: %v", err)
			if retryErr := broker.HandleRetry(ctx, ch, &msg); retryErr != nil {
				logging.Errorf(ctx, nil, "Failed to handle retry for refund: %v", retryErr)
			}
		} else {
			_ = msg.Ack(false)
		}
	}()

	payload := &broker.OrderRefundPayload{}
	if err = json.Unmarshal(msg.Body, payload); err != nil {
		err = errors.Wrap(err, "failed to unmarshal refund payload")
		return
	}

	_, err = c.app.Commands.RefundPayment.Handle(ctx, command.RefundPayment{
		OrderID:         payload.OrderID,
		CustomerID:      payload.CustomerID,
		PaymentIntentID: payload.PaymentIntentID,
	})
	if err != nil {
		logging.Errorf(ctx, nil, "Failed to refund order %s: %v", payload.OrderID, err)
	}
}

func (c *Consumer) handleMessage(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	logrus.Infof("Payment received message: %s from %s exchange with routing key %s", string(msg.Body), msg.Exchange, msg.RoutingKey)

	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers), fmt.Sprintf("rabbitmq.%s.consume", q.Name))
	defer span.End()

	logging.Infof(ctx, nil, "Payment received message: %s from %s exchange with routing key %s", string(msg.Body), msg.Exchange, msg.RoutingKey)
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

	o := &entity.Order{}
	if err = json.Unmarshal(msg.Body, o); err != nil {
		err = errors.Wrap(err, "failed to unmarshal message body")
		span.RecordError(err)
		return
	}

	if _, err = c.app.Commands.CreatePayment.Handle(ctx, command.CreatePayment{Order: o}); err != nil {
		// 判断是否是永久性错误 (不该重试的那种)
		//   - 状态机冲突: 订单已 CANCELLED, 重试无意义
		//   - 订单不存在: 同上
		// 这种情况下直接 ack 丢弃,别让 HandleRetry 把消息放回队列反复折磨
		errStr := err.Error()
		if strings.Contains(errStr, "cannot transit") ||
			strings.Contains(errStr, "order not found") ||
			strings.Contains(errStr, "NotFound") {
			logging.Warnf(ctx, nil, "permanent error for order %s, dropping message: %v", o.ID, err)
			err = nil // 清空 err,让 defer 走 Ack 分支
			return
		}

		err = errors.Wrap(err, "failed to create payment handler")
		if retryErr := broker.HandleRetry(ctx, ch, &msg); retryErr != nil {
			logging.Warnf(ctx, nil, "Failed to handle retry: %v", retryErr)
		}
		return
	}

	span.AddEvent("payment.created")
}
