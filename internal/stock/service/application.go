package service

import (
	"context"

	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/common/metrics"
	"github.com/ecstasoy/gorder/stock/adapters"
	"github.com/ecstasoy/gorder/stock/app"
	"github.com/ecstasoy/gorder/stock/app/command"
	"github.com/ecstasoy/gorder/stock/app/query"
	"github.com/ecstasoy/gorder/stock/infra/integration"
	"github.com/ecstasoy/gorder/stock/infra/persistent"
	"github.com/sirupsen/logrus"
)

func NewApplication(_ context.Context) app.Application {
	db := persistent.NewMySQL()
	stockRepo := adapters.NewMySQLStockRepository(db)
	stripeAPI := integration.NewStripeAPI()
	metricsClient := metrics.TodoMetrics{}
	redis.Init()
	redisClient := redis.LocalClient()
	return app.Application{
		Commands: app.Commands{
			RestoreStock:     command.NewRestoreStockHandler(stockRepo, logrus.StandardLogger(), metricsClient),
			WarmUpFlashStock: command.NewWarmUpFlashStockHandler(redisClient, logrus.StandardLogger(), metricsClient),
		},
		Queries: app.Queries{
			CheckIfItemsInStock: query.NewCheckIfItemsInStockHandler(stockRepo, stripeAPI, logrus.StandardLogger(), metricsClient),
			GetItems:            query.NewGetItemsHandler(stockRepo, logrus.StandardLogger(), metricsClient),
		},
	}
}
