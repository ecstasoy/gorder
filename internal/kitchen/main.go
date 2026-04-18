package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/ecstasoy/gorder/common/broker"
	grpcClient "github.com/ecstasoy/gorder/common/client"
	_ "github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/common/tracing"
	"github.com/ecstasoy/gorder/kitchen/adapters"
	"github.com/ecstasoy/gorder/kitchen/infra/consumer"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func init() {
	logging.Init()
}

func main() {
	serviceName := viper.GetString("kitchen.service-name")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdown, err := tracing.InitTracerProvider(viper.GetString("jaeger.url"), serviceName)
	if err != nil {
		logrus.Fatalf("failed to initialize tracer provider: %v", err)
	}
	defer shutdown(ctx)

	orderClient, err := grpcClient.NewOrderGRPCClient(ctx)
	if err != nil {
		logrus.Fatalf("failed to create order gRPC client: %v", err)
	}
	defer grpcClient.CloseOrderClient()

	_, ch, closeCh := broker.Connect(
		viper.GetString("rabbitmq.user"),
		viper.GetString("rabbitmq.password"),
		viper.GetString("rabbitmq.host"),
		viper.GetString("rabbitmq.port"),
	)
	defer func() {
		_ = closeCh()
	}()

	orderGRPC := adapters.NewOrderGRPC(orderClient)
	go consumer.NewConsumer(orderGRPC).Listen(ch)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigs
		logrus.Info("receive signal, existing...")
		os.Exit(0)
	}()
	logrus.Println("Kitchen service started, waiting for messages...")
	select {}
}
