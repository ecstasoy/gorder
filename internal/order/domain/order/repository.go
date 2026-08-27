package order

import (
	"context"
	"time"
)

type Repository interface {
	Create(context.Context, *Order) (*Order, error)
	Get(ctx context.Context, id, customerID string) (*Order, error)
	Update(ctx context.Context,
		o *Order,
		updateFunc func(context.Context, *Order) (*Order, error),
	) error

	// ListTerminalOlderThan 返回 _id 时间戳早于 cutoff、状态为 PAID 或 CANCELLED
	// 的订单,按时间正序返回最多 limit 条。
	//
	// 用途:reservation 对账 worker 扫描"已到终态但 stock 侧 reservation 可能
	// 没跟上"的订单 —— ADR-0001 留下的 follow-up,见 docs/adr-0001-summary.md
	// "后续工作 #5"。
	//
	// 实现细节:Mongo ObjectID 自身的 4-byte 时间戳前缀就是创建时间,不需要
	// 在 schema 上新加 created_at/updated_at 字段。
	ListTerminalOlderThan(ctx context.Context, cutoff time.Time, limit int64) ([]*Order, error)
}

type NotFoundError struct {
	OrderID string
}

func (e *NotFoundError) Error() string {
	return "order not found: " + e.OrderID
}
