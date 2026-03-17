package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/sirupsen/logrus"
)

type CheckIfItemsInStock struct {
	Items []*orderpb.ItemWithQuantity
}

type CheckIfItemsInStockHandler decorator.QueryHandler[CheckIfItemsInStock, []*orderpb.Item]

type checkIfItemsInStockHandler struct {
	stockRepo domain.Repository
}

func NewCheckIfItemsInStockHandler(
	stockRepo domain.Repository,
	logger *logrus.Entry,
	metricsClient decorator.MetricsClient,
) CheckIfItemsInStockHandler {
	if stockRepo == nil {
		panic("stockRepo cannot be nil")
	}
	return decorator.ApplyQueryDecorators[CheckIfItemsInStock, []*orderpb.Item](
		checkIfItemsInStockHandler{stockRepo: stockRepo},
		logger,
		metricsClient,
	)
}

// TODO: This is a stub implementation. Replace with actual logic to check stock and get price IDs.
var stub = map[string]string{
	"1": "price_1TBQcBBMfcQ39REZYgwPgjcd",
	"2": "price_1TBQc1BMfcQ39REZWyhzULGw",
}

func (c checkIfItemsInStockHandler) Handle(ctx context.Context, query CheckIfItemsInStock) ([]*orderpb.Item, error) {
	var res []*orderpb.Item
	for _, item := range query.Items {
		priceId, ok := stub[item.ItemID]
		if !ok {
			priceId = stub["1"]
		}
		res = append(res, &orderpb.Item{
			ID:       item.ItemID,
			Quantity: item.Quantity,
			PriceID:  priceId,
		})
	}
	return res, nil
}
