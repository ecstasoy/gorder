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
	orderClient, err := grpcClient.NewOrderGRPCClient(ctx)
	if err != nil {
		panic(err)
	}
	orderGRPC := adapters.NewOrderGRPC(orderClient)
	//memoryProcessor := processor.NewMemoryProcessor()
	stripeProcessor := processor.NewStripeProcessor(config.GetStringWithEnv("stripe-key"))
	return newApplication(ctx, orderGRPC, stripeProcessor), func() {
		_ = grpcClient.CloseOrderClient()
	}
}

func newApplication(ctx context.Context, grpc *adapters.OrderGRPC, processor domain.Processor) app.Application {
	metricsClient := metrics.TodoMetrics{}
	return app.Application{
		Commands: app.Commands{
			CreatePayment: command.NewCreatePaymentHandler(processor, grpc, logrus.StandardLogger(), metricsClient),
			RefundPayment: command.NewRefundPaymentHandler(processor, logrus.StandardLogger(), metricsClient),
		},
	}
}
