package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	stockGRPC "github.com/ecstasoy/gorder/order/adapters/grpc"
)

type StockService interface {
	GetItems(ctx context.Context, itemIDs []string) ([]*orderpb.Item, error)
	WarmUpFlashStock(ctx context.Context, items []*orderpb.ItemWithQuantity, ttlSeconds int64) error
	// ADR-0001 Step 4 + ADR-0002 — saga 通过这三个方法控制 reservation lifecycle。
	// 取代了 Step 7 删除的 DeductStock / RestoreStock / CheckIfItemsInStock。
	Reserve(ctx context.Context, orderID string, items []*orderpb.ItemWithQuantity) error
	Confirm(ctx context.Context, orderID string) error
	Release(ctx context.Context, orderID string) error

	// ADR-0004 — activity entity。CreateActivity 创建 draft 活动,
	// WarmUpActivity 推 Redis 到 active,GetActivity 查 product_id / 状态。
	CreateActivity(ctx context.Context, name, productID string, totalStock int32, startUnix, endUnix int64) (string, error)
	WarmUpActivity(ctx context.Context, activityID string) (bool, error)
	GetActivity(ctx context.Context, activityID string) (*stockGRPC.ActivityInfo, error)
}
