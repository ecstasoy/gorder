package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/sirupsen/logrus"
)

type RestoreStock struct {
	Items []*entity.ItemWithQuantity
}

type RestoreStockHandler decorator.CommandHandler[RestoreStock, struct{}]

type restoreStockHandler struct {
	stockRepo domain.Repository
}

func NewRestoreStockHandler(
	stockRepo domain.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) RestoreStockHandler {
	if stockRepo == nil {
		panic("stockRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[RestoreStock, struct{}](
		restoreStockHandler{stockRepo: stockRepo},
		logger,
		metricsClient,
	)
}

func (h restoreStockHandler) Handle(ctx context.Context, cmd RestoreStock) (struct{}, error) {
	return struct{}{}, h.stockRepo.RestoreStock(ctx, cmd.Items)
}
