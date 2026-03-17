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
			c.handleMessage(msg)
		}
	}()

	<-forever
}

func (c *Consumer) handleMessage(msg amqp.Delivery) {
	logrus.Infof("Payment received message: %s from %s exchange with routing key %s", string(msg.Body), msg.Exchange, msg.RoutingKey)

	o := &orderpb.Order{}
	if err := json.Unmarshal(msg.Body, o); err != nil {
		logrus.Infof("failed to unmarshal order: %w", err)
		_ = msg.Nack(false, false)
		return
	}

	if _, err := c.app.Commands.CreatePayment.Handle(context.TODO(), command.CreatePayment{Order: o}); err != nil {
		//TODO: retry logic
		logrus.Infof("failed to handle order: %w", err)
		_ = msg.Nack(false, false)
	}

	_ = msg.Ack(false)
	logrus.Info("Payment consumed message successfully")
}
