package main

import (
	"context"
	"log"

	"github.com/ecstasoy/gorder/common/broker"
	_ "github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/discovery"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/common/server"
	"github.com/ecstasoy/gorder/common/tracing"
	"github.com/ecstasoy/gorder/order/infra/consumer"
	"github.com/ecstasoy/gorder/order/infra/outbox"
	"github.com/ecstasoy/gorder/order/ports"
	"github.com/ecstasoy/gorder/order/service"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

func init() {
	logging.Init()
}

func main() {
	serviceName := viper.GetString("order.service-name")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdown, err := tracing.InitTracerProvider(viper.GetString("jaeger.url"), serviceName)
	if err != nil {
		logrus.Fatalf("failed to initialize tracer provider: %v", err)
	}
	defer shutdown(ctx)

	application, stockGRPC, redisClient, outboxRepo, cleanup := service.NewApplication(ctx)
	defer cleanup()

	conn, ch, closeCh := broker.Connect(
		viper.GetString("rabbitmq.user"),
		viper.GetString("rabbitmq.password"),
		viper.GetString("rabbitmq.host"),
		viper.GetString("rabbitmq.port"),
	)
	defer func() {
		_ = closeCh()
	}()

	// 两个 consumer 必须各自用独立的 channel — AMQP channel 不是 goroutine-safe,
	// 共用会出现 503 "unexpected command received" 之类的协议错误。
	orderPaidCh, err := conn.Channel()
	if err != nil {
		logrus.Fatalf("failed to open order paid consumer channel: %v", err)
	}
	defer orderPaidCh.Close()

	flashSaleCh, err := conn.Channel()
	if err != nil {
		logrus.Fatalf("failed to open flash sale consumer channel: %v", err)
	}
	defer flashSaleCh.Close()

	outboxCh, err := conn.Channel()
	if err != nil {
		logrus.Fatalf("failed to open outbox publisher channel: %v", err)
	}
	defer outboxCh.Close()

	publisher := broker.NewRabbitMQPublisher(ch)
	c := consumer.NewConsumer(application, redisClient, publisher)
	go c.Listen(orderPaidCh)
	go c.ListenFlashSaleOrders(flashSaleCh)

	// ADR-0001 Step 2: order.created + payment.delay 走 outbox。
	// outboxRepo 由 service.NewApplication 创建并注入 OrderDomainService;此处只装 worker。
	outboxPublisher := broker.NewRabbitMQPublisher(outboxCh)
	outboxWorker := outbox.NewWorker(outboxRepo, outboxPublisher)
	go outboxWorker.Run(ctx)

	deregisterFunc, err := discovery.RegisterToConsul(ctx, serviceName)

	if err != nil {
		logrus.Fatalf("failed to register to consul: %v", err)
	}
	defer func() {
		_ = deregisterFunc()
	}()

	go server.RunGRPCServer(serviceName, func(server *grpc.Server) {
		orderpb.RegisterOrderServiceServer(server, ports.NewGRPCServer(application))
	})

	server.RunHTTPServer(serviceName, func(router *gin.Engine) {
		router.StaticFile("/payment/success", "../../public/success.html")
		ports.RegisterHandlersWithOptions(router, HTTPServer{
			app: application,
		}, ports.GinServerOptions{
			BaseURL:      "/api",
			Middlewares:  nil,
			ErrorHandler: nil,
		})
		flashServer := FlashSaleHTTPServer{
			app:       application,
			stockGRPC: stockGRPC,
			publisher: publisher,
		}
		router.POST("/flash-sale/warmup", flashServer.PostFlashSaleWarmup) // Deprecated (ADR-0004)
		router.POST("/flash-sale/orders", flashServer.PostFlashSaleOrders)
		router.GET("/flash-sale/result/:token", flashServer.GetFlashSaleResult)
		// ADR-0004 activity-driven endpoints
		router.POST("/flash-sale/activities", flashServer.PostCreateActivity)
		router.POST("/flash-sale/activities/:activity_id/warmup", flashServer.PostWarmUpActivity)
		router.POST("/flash-sale/activities/:activity_id/orders", flashServer.PostActivityFlashSaleOrder)
	})
	log.Printf("%v", viper.Get("order"))
}
