package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/genproto/stockpb"
)

type StockService interface {
	CheckIfItemsInStock(ctx context.Context, items []*orderpb.ItemWithQuantity) (*stockpb.CheckIfItemsInStockResponse, error)
	GetItems(ctx context.Context, itemIDs []string) ([]*orderpb.Item, error)
	RestoreStock(ctx context.Context, items []*orderpb.ItemWithQuantity) error
	WarmUpFlashStock(ctx context.Context, items []*orderpb.ItemWithQuantity, ttlSeconds int64) error
	DeductStock(ctx context.Context, items []*orderpb.ItemWithQuantity) error
}
