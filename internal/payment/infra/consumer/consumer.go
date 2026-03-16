package consumer

import (
	"fmt"

	"github.com/ecstasoy/gorder/common/broker"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
)

type Consumer struct {
}

func NewConsumer() *Consumer {
	return &Consumer{}
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
	_ = msg.Ack(false)
}
