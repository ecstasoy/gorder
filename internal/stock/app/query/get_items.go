package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/sirupsen/logrus"
)

type GetItems struct {
	ItemIDs []string
}

type GetItemsHandler decorator.QueryHandler[GetItems, []*orderpb.Item]

type getItemsHandler struct {
	stockRepo domain.Repository
}

func NewGetItemsHandler(
	stockRepo domain.Repository,
	logger *logrus.Logger,
	metricClient decorator.MetricsClient,
) GetItemsHandler {
	if stockRepo == nil {
		panic("stockRepo cannot be nil")
	}
	return decorator.ApplyQueryDecorators[GetItems, []*orderpb.Item](
		getItemsHandler{stockRepo: stockRepo},
		logger,
		metricClient,
	)
}

func (g getItemsHandler) Handle(ctx context.Context, query GetItems) ([]*orderpb.Item, error) {
	items, err := g.stockRepo.GetItems(ctx, query.ItemIDs)
	if err != nil {
		return nil, err
	}
	return convertor.NewItemConvertor().EntitiesToProtos(items), nil
}
