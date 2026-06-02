package reservation

import (
	"context"
	"fmt"

	"github.com/ecstasoy/gorder/common/entity"
)

// Repository 是 stock 服务的 reservation 生命周期能力 —— ADR-0001 Step 4
// 引入的 Reserve / Confirm / Release 三态机的持久化接口。
//
// OrderID 是幂等键 —— Reserve / Confirm / Release 同一 OrderID 重复调,
// 已到终态的调用是 no-op,在中间态调用会触发状态转移。
type Repository interface {
	Reserve(ctx context.Context, orderID string, items []*entity.ItemWithQuantity) error
	Confirm(ctx context.Context, orderID string) error
	Release(ctx context.Context, orderID string) error
}

// NotFoundError 在 Confirm 调用时,目标 OrderID 没有任何 reservation 记录。
// Release 走 no-op (从未 Reserve 过的订单 release 是合法的),所以这个错误
// 主要由 Confirm 路径返回。
type NotFoundError struct {
	OrderID string
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("reservation: not found for order %s", e.OrderID)
}

// ConflictError 表示状态机不允许该转移 —— 比如 Reserve 一个已经 confirmed
// 的 OrderID,或 Release 一个已经 confirmed 的 OrderID (后者要走 refund 而
// 不是 release)。
type ConflictError struct {
	OrderID string
	Current string // 当前 status: "held" / "confirmed" / "released"
	Wanted  string // 想要的动作: "reserve" / "confirm" / "release"
}

func (e ConflictError) Error() string {
	return fmt.Sprintf("reservation: order %s is %s, cannot %s", e.OrderID, e.Current, e.Wanted)
}

// InsufficientStockError 在 Reserve 时库存不足。返回时所有已扣的同 OrderID
// 其他 item 会被 transaction 回滚 —— 即"全或无"语义,见 ADR-0001 问题 3。
type InsufficientStockError struct {
	ProductID string
	Want      int32
}

func (e InsufficientStockError) Error() string {
	return fmt.Sprintf("reservation: insufficient stock for %s (want %d)", e.ProductID, e.Want)
}
