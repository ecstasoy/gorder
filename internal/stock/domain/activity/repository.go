package activity

import "context"

// Repository 是 activity aggregate 的持久化接口。
type Repository interface {
	Create(ctx context.Context, a *Activity) error
	Get(ctx context.Context, id string) (*Activity, error)
	// Update 在 caller 拿到 aggregate 修改字段后写回(全字段覆盖)。
	// 用于 MarkWarmedUp 之后的持久化,以及未来手动状态变更。
	Update(ctx context.Context, a *Activity) error
}
