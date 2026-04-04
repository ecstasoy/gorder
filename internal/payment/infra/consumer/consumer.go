package consumer

import (
	"context"
	"encoding/json"
	"fmt"

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

func (c *Consumer) Listen(ch *amqp.Channel) {
	q, err := ch.QueueDeclare(broker.EventOrderCreated, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare queue: %w", err))
	}
	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Warnf("failed to consume message: %w", err)
	}

	refundQ, err := ch.QueueDeclare(broker.EventOrderRefund, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare refund queue: %w", err))
	}
	refundMsgs, err := ch.Consume(refundQ.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to consume refund queue: %w", err))
	}

	logrus.Infof("Payment consumer started, listening on queues: %s, %s", broker.EventOrderCreated, broker.EventOrderRefund)

	var forever chan struct{}
	go func() {
		for msg := range msgs {
			c.handleMessage(ch, msg, q)
		}
	}()
	go func() {
		for msg := range refundMsgs {
			c.handleRefund(ch, msg, refundQ)
		}
	}()

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
	if err := json.Unmarshal(msg.Body, o); err != nil {
		err = errors.Wrap(err, "failed to unmarshal message body")
		span.RecordError(err)
		return
	}

	if _, err = c.app.Commands.CreatePayment.Handle(ctx, command.CreatePayment{Order: o}); err != nil {
		err = errors.Wrap(err, "failed to create payment handler")
		if retryErr := broker.HandleRetry(ctx, ch, &msg); retryErr != nil {
			logging.Warnf(ctx, nil, "Failed to handle retry: %v", retryErr)
		}
		return
	}

	span.AddEvent("payment.created")
}
