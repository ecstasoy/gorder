package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/sirupsen/logrus"
)

type DeductStock struct {
	Items []*entity.ItemWithQuantity
}

type DeductStockHandler decorator.CommandHandler[DeductStock, struct{}]

type deductStockHandler struct {
	stockRepo domain.Repository
}

func NewDeductStockHandler(
	stockRepo domain.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) DeductStockHandler {
	if stockRepo == nil {
		panic("stockRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[DeductStock, struct{}](
		deductStockHandler{stockRepo: stockRepo},
		logger,
		metricsClient,
	)
}

func (h deductStockHandler) Handle(ctx context.Context, cmd DeductStock) (struct{}, error) {
	if err := h.stockRepo.DeductStock(ctx, cmd.Items); err != nil {
		return struct{}{}, err
	}
	return struct{}{}, nil
}
