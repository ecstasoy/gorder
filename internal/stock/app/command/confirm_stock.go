package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/stock/domain/reservation"
	"github.com/sirupsen/logrus"
)

type ConfirmStock struct {
	OrderID string
}

type ConfirmStockHandler decorator.CommandHandler[ConfirmStock, struct{}]

type confirmStockHandler struct {
	reservationRepo reservation.Repository
}

func NewConfirmStockHandler(
	reservationRepo reservation.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) ConfirmStockHandler {
	if reservationRepo == nil {
		panic("reservationRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[ConfirmStock, struct{}](
		confirmStockHandler{reservationRepo: reservationRepo},
		logger,
		metricsClient,
	)
}

func (h confirmStockHandler) Handle(ctx context.Context, cmd ConfirmStock) (struct{}, error) {
	return struct{}{}, h.reservationRepo.Confirm(ctx, cmd.OrderID)
}
