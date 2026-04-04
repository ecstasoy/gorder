package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/broker"
	grpcClient "github.com/ecstasoy/gorder/common/client"
	"github.com/ecstasoy/gorder/common/metrics"
	"github.com/ecstasoy/gorder/order/adapters"
	"github.com/ecstasoy/gorder/order/adapters/grpc"
	"github.com/ecstasoy/gorder/order/app"
	"github.com/ecstasoy/gorder/order/app/command"
	"github.com/ecstasoy/gorder/order/app/query"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func NewApplication(ctx context.Context) (app.Application, query.StockService, func()) {
	stockClient, err := grpcClient.NewStockGRPCClient(ctx)
	if err != nil {
		panic(err)
	}
	ch, closeCh := broker.Connect(
		viper.GetString("rabbitmq.user"),
		viper.GetString("rabbitmq.password"),
		viper.GetString("rabbitmq.host"),
		viper.GetString("rabbitmq.port"),
	)
	stockGRPC := grpc.NewStockGRPC(stockClient)
	return newApplication(ctx, stockGRPC, ch), stockGRPC, func() {
		_ = grpcClient.CloseStockClient()
		_ = closeCh()
		_ = ch.Close()
	}
}

func newApplication(_ context.Context, stockGRPC query.StockService, ch *amqp.Channel) app.Application {
	mongoClient := newMongoClient()
	orderRepo := adapters.NewOrderRepositoryMongo(mongoClient)
	metricsClient := metrics.TodoMetrics{}
	return app.Application{
		Commands: app.Commands{
			CreateOrder: command.NewCreateOrderHandler(orderRepo, stockGRPC, ch, logrus.StandardLogger(), metricsClient),
			UpdateOrder: command.NewUpdateOrderHandler(orderRepo, logrus.StandardLogger(), metricsClient),
			CancelOrder: command.NewCancelOrderHandler(orderRepo, stockGRPC, logrus.StandardLogger(), metricsClient),
		},
		Queries: app.Queries{
			GetCustomerOrder: query.NewGetCustomerOrderHandler(orderRepo, logrus.StandardLogger(), metricsClient),
		},
	}
}

func newMongoClient() *mongo.Client {
	uri := fmt.Sprintf(
		"mongodb://%s:%s@%s:%d/?authSource=admin",
		viper.GetString("mongo.user"),
		viper.GetString("mongo.password"),
		viper.GetString("mongo.host"),
		viper.GetInt("mongo.port"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		panic(fmt.Errorf("failed to connect to MongoDB: %w", err))
	}

	if err = c.Ping(ctx, readpref.Primary()); err != nil {
		panic(fmt.Errorf("failed to ping MongoDB: %w", err))
	}

	logrus.Infof("Successfully connected to MongoDB at %s", uri)
	return c
}
