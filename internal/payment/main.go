package main

import (
	"context"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/common/server"
	"github.com/ecstasoy/gorder/payment/infra/consumer"
	"github.com/ecstasoy/gorder/payment/service"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func init() {
	logging.Init()
	if err := config.NewViperConfig(); err != nil {
		logrus.Fatal(err)
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverType := viper.GetString("payment.server-to-run")

	application, cleanup := service.NewApplication(ctx)
	defer cleanup()

	ch, closeCh := broker.Connect(
		viper.GetString("rabbitmq.user"),
		viper.GetString("rabbitmq.password"),
		viper.GetString("rabbitmq.host"),
		viper.GetString("rabbitmq.port"),
	)

	defer func() {
		_ = closeCh()
		_ = ch.Close()
	}()

	go consumer.NewConsumer(application).Listen(ch)

	paymentHandler := NewPaymentHandler(ch)
	switch serverType {
	case "http":
		server.RunHTTPServer(viper.GetString("payment.service-name"), paymentHandler.RegisterRoutes)
	case "grpc":
		logrus.Panic("grpc server is not implemented for payment service")
	default:
		logrus.Panicf("unknown server type: %s", serverType)
	}
}
