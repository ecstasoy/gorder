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

func NewApplication(_ context.Context) (app.Application, func()) {
	db := persistent.NewMySQL()
	stockRepo := adapters.NewMySQLStockRepository(db)
	reservationRepo := adapters.NewMySQLReservationRepository(db)
	stripeAPI := integration.NewStripeAPI()
	metricsClient := metrics.NewPrometheusMetricsClient()
	redis.Init()
	redisClient := redis.LocalClient()
	logger := logrus.StandardLogger()

	application := app.Application{
		Commands: app.Commands{
			WarmUpFlashStock: command.NewWarmUpFlashStockHandler(stockRepo, stripeAPI, redisClient, logger, metricsClient),
			ReserveStock:     command.NewReserveStockHandler(reservationRepo, logger, metricsClient),
			ConfirmStock:     command.NewConfirmStockHandler(reservationRepo, logger, metricsClient),
			ReleaseStock:     command.NewReleaseStockHandler(reservationRepo, logger, metricsClient),
		},
		Queries: app.Queries{
			GetItems: query.NewGetItemsHandler(stripeAPI, logger, metricsClient),
		},
	}

	return application, func() {}
}
