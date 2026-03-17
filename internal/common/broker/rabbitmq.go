package broker

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
)

func Connect(user, pwd, host, port string) (*amqp.Channel, func() error) {
	address := fmt.Sprintf("amqp://%s:%s@%s:%s/", user, pwd, host, port)
	logrus.Infof("Connecting to RabbitMQ at %s", address)

	conn, err := amqp.Dial(address)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to connect to rabbitmq: %w", err))
	}

	ch, err := conn.Channel()
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to open channel: %w", err))
	}

	err = ch.ExchangeDeclare(EventOrderCreated, "direct", true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare %s exchange: %w", EventOrderCreated, err))
	}

	err = ch.ExchangeDeclare(EventOrderPaid, "fanout", true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare %s exchange: %w", EventOrderPaid, err))
	}

	logrus.Info("Successfully connected to RabbitMQ")
	return ch, ch.Close
}
