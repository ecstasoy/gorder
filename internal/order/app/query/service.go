package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type StockService interface {
	GetItems(ctx context.Context, itemIDs []string) ([]*orderpb.Item, error)
	WarmUpFlashStock(ctx context.Context, items []*orderpb.ItemWithQuantity, ttlSeconds int64) error
	// ADR-0001 Step 4 + ADR-0002 — saga 通过这三个方法控制 reservation lifecycle。
	// 取代了 Step 7 删除的 DeductStock / RestoreStock / CheckIfItemsInStock。
	Reserve(ctx context.Context, orderID string, items []*orderpb.ItemWithQuantity) error
	Confirm(ctx context.Context, orderID string) error
	Release(ctx context.Context, orderID string) error
}
