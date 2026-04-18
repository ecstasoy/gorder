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

	application, stockGRPC, redisClient, cleanup := service.NewApplication(ctx)
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

	c := consumer.NewConsumer(application, redisClient)
	go c.Listen(orderPaidCh)
	go c.ListenFlashSaleOrders(flashSaleCh)

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
			amqpCh:    ch,
		}
		router.POST("/flash-sale/warmup", flashServer.PostFlashSaleWarmup)
		router.POST("/flash-sale/orders", flashServer.PostFlashSaleOrders)
		router.GET("/flash-sale/result/:token", flashServer.GetFlashSaleResult)
	})
	log.Printf("%v", viper.Get("order"))
}
