package consumer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/order/app"
	"github.com/ecstasoy/gorder/order/app/command"
	domain "github.com/ecstasoy/gorder/order/domain/order"
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
		logrus.Warnf("failed to consume message: %w", err)
	}

	logrus.Infof("Order consumer started, listening on queue: %s", broker.EventOrderPaid)

	var forever chan struct{}
	go func() {
		for msg := range msgs {
			c.handleMessage(msg)
		}
	}()

	<-forever
}

func (c *Consumer) handleMessage(msg amqp.Delivery) {
	logrus.Infof("Order received paid message: %s from %s", string(msg.Body), msg.Exchange)

	o := &domain.Order{}
	if err := json.Unmarshal(msg.Body, o); err != nil {
		logrus.Errorf("failed to unmarshal order: %v", err)
		_ = msg.Nack(false, false)
		return
	}

	logrus.Infof("Processing paid order: ID=%s, CustomerID=%s, Status=%v", o.ID, o.CustomerID, o.Status)

	_, err := c.app.Commands.UpdateOrder.Handle(context.TODO(), command.UpdateOrder{
		Order: o,
		UpdateFunc: func(ctx context.Context, existingOrder *domain.Order) (*domain.Order, error) {
			existingOrder.Status = o.Status
			existingOrder.PaymentLink = o.PaymentLink
			return existingOrder, nil
		},
	})

	if err != nil {
		logrus.Errorf("failed to update order: %v", err)
		_ = msg.Nack(false, false)
		return
	}

	_ = msg.Ack(false)
	logrus.Infof("Order consumed paid message successfully for order: %s", o.ID)
}
