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
	"github.com/spf13/viper"
)

func NewApplication(ctx context.Context) (app.Application, func()) {
	orderClient, err := grpcClient.NewOrderGRPCClient(ctx)
	if err != nil {
		panic(err)
	}
	orderGRPC := adapters.NewOrderGRPC(orderClient)

	// payment.processor 配置切换 processor:"stripe" (真实 Stripe API) 或 "mem" (内存 stub,压测用)
	// 默认 stripe 保持原有行为
	var proc domain.Processor
	switch viper.GetString("payment.processor") {
	case "mem":
		logrus.Info("Payment: using in-memory processor (stub, for load testing)")
		proc = processor.NewMemoryProcessor()
	default:
		logrus.Info("Payment: using Stripe processor")
		proc = processor.NewStripeProcessor(config.GetStringWithEnv("stripe-key"))
	}

	return newApplication(ctx, orderGRPC, proc), func() {
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
