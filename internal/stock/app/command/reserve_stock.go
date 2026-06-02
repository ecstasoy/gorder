package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/stock/domain/reservation"
	"github.com/sirupsen/logrus"
)

type ReserveStock struct {
	OrderID string
	Items   []*entity.ItemWithQuantity
}

type ReserveStockHandler decorator.CommandHandler[ReserveStock, struct{}]

type reserveStockHandler struct {
	reservationRepo reservation.Repository
}

func NewReserveStockHandler(
	reservationRepo reservation.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) ReserveStockHandler {
	if reservationRepo == nil {
		panic("reservationRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[ReserveStock, struct{}](
		reserveStockHandler{reservationRepo: reservationRepo},
		logger,
		metricsClient,
	)
}

func (h reserveStockHandler) Handle(ctx context.Context, cmd ReserveStock) (struct{}, error) {
	return struct{}{}, h.reservationRepo.Reserve(ctx, cmd.OrderID, cmd.Items)
}
