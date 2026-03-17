package service

import (
	"context"

	grpcClient "github.com/ecstasoy/gorder/common/client"
	"github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/metrics"
	"github.com/ecstasoy/gorder/payment/adapters"
	"github.com/ecstasoy/gorder/payment/app"
	"github.com/ecstasoy/gorder/payment/app/command"
	"github.com/ecstasoy/gorder/payment/domain"
	"github.com/ecstasoy/gorder/payment/infra/processor"
	"github.com/sirupsen/logrus"
)

func NewApplication(ctx context.Context) (app.Application, func()) {
	orderClient, closeOrderClient, err := grpcClient.NewOrderGRPCClient(ctx)
	if err != nil {
		panic(err)
	}
	orderGRPC := adapters.NewOrderGRPC(orderClient)
	//memoryProcessor := processor.NewMemoryProcessor()
	stripeProcessor := processor.NewStripeProcessor(config.GetStringWithEnv("stripe-key"))
	return newApplication(ctx, orderGRPC, stripeProcessor), func() {
		_ = closeOrderClient()
	}
}

func newApplication(ctx context.Context, grpc *adapters.OrderGRPC, processor domain.Processor) app.Application {
	logger := logrus.NewEntry(logrus.StandardLogger())
	metricsClient := metrics.TodoMetrics{}
	return app.Application{
		Commands: app.Commands{
			CreatePayment: command.NewCreatePaymentHandler(processor, grpc, logger, metricsClient),
		},
	}
}
