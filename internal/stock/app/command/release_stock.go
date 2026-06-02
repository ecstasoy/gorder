package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/stock/domain/reservation"
	"github.com/sirupsen/logrus"
)

type ReleaseStock struct {
	OrderID string
}

type ReleaseStockHandler decorator.CommandHandler[ReleaseStock, struct{}]

type releaseStockHandler struct {
	reservationRepo reservation.Repository
}

func NewReleaseStockHandler(
	reservationRepo reservation.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) ReleaseStockHandler {
	if reservationRepo == nil {
		panic("reservationRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[ReleaseStock, struct{}](
		releaseStockHandler{reservationRepo: reservationRepo},
		logger,
		metricsClient,
	)
}

func (h releaseStockHandler) Handle(ctx context.Context, cmd ReleaseStock) (struct{}, error) {
	return struct{}{}, h.reservationRepo.Release(ctx, cmd.OrderID)
}
