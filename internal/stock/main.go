package main

import (
	"context"

	"github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/genproto/stockpb"
	"github.com/ecstasoy/gorder/common/server"
	"github.com/ecstasoy/gorder/stock/ports"
	"github.com/ecstasoy/gorder/stock/service"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

func init() {
	if err := config.NewViperConfig(); err != nil {
		logrus.Fatal(err)
	}
}

func main() {
	serviceName := viper.GetString("stock.service-name")
	serverType := viper.GetString("stock.server-to-run")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	application := service.NewApplication(ctx)
	switch serverType {
	case "grpc":
		server.RunGRPCServer(serviceName, func(server *grpc.Server) {
			stockpb.RegisterStockServiceServer(server, ports.NewGRPCServer(application))
		})
	case "http":
		server.RunHTTPServer(serviceName, func(router *gin.Engine) {
			ports.RegisterHandlersWithOptions(router, HTTPServer{
				app: application,
			}, ports.GinServerOptions{
				BaseURL:      "/api",
				Middlewares:  nil,
				ErrorHandler: nil,
			})
		})
	default:
		panic("unknown server type")
	}
}
