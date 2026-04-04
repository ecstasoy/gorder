package consumer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/payment/app"
	"github.com/ecstasoy/gorder/payment/app/command"
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

	var forever chan struct{}
	go func() {
		for msg := range msgs {
			c.handleMessage(ch, msg, q)
		}
	}()

	<-forever
}

func (c *Consumer) handleMessage(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	logrus.Infof("Payment received message: %s from %s exchange with routing key %s", string(msg.Body), msg.Exchange, msg.RoutingKey)

	ctx := broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers)
	t := otel.Tracer("rabbitmq")
	_, span := t.Start(ctx, fmt.Sprintf("rabbitmq.%s.consume", q.Name))
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			_ = msg.Nack(false, false)
		} else {
			_ = msg.Ack(false)
		}
	}()

	o := &orderpb.Order{}
	if err := json.Unmarshal(msg.Body, o); err != nil {
		logrus.Infof("failed to unmarshal order: %w", err)
		span.RecordError(err)
		return
	}

	if _, err := c.app.Commands.CreatePayment.Handle(ctx, command.CreatePayment{Order: o}); err != nil {
		if err = broker.HandleRetry(ctx, ch, msg); err != nil {
			logrus.Warnf("failed to handle retry message: %s, messageID: %s, error: %v", msg.Body, msg.MessageId, err)
		}
		logrus.Infof("failed to handle order: %w", err)
		span.RecordError(err)
		return
	}

	span.AddEvent("payment.created")
	logrus.Info("Payment consumed message successfully")
}
