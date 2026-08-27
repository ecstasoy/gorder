// Package activity 是 ADR-0004 引入的 flash sale 活动 aggregate。
//
// 把"活动"从隐式(Redis TTL)升级为一级 entity:
//   - 同 SKU 可承载多场活动
//   - Order 上记录 activity_id,审计 / 退款 / 报表按活动拉取
//   - 状态机保证 warmup 不可重做 + 活动时段约束
package activity

import (
	"errors"
	"time"
)

// Status 是 activity 生命周期的状态机。
type Status string

const (
	StatusDraft     Status = "draft"     // 创建但未排期
	StatusScheduled Status = "scheduled" // 已排期,等 warmup
	StatusActive    Status = "active"    // warmup 完成,可下单
	StatusEnded     Status = "ended"     // end_time 到期或人工结束
	StatusCancelled Status = "cancelled" // 主动取消(运维)
)

// Activity 是 flash sale 活动 aggregate root。
type Activity struct {
	ID         string
	Name       string
	ProductID  string
	TotalStock int32
	StartTime  time.Time
	EndTime    time.Time
	Status     Status
	WarmupDone bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewActivity 创建一个 draft 状态的活动。
func NewActivity(id, name, productID string, totalStock int32, start, end time.Time) (*Activity, error) {
	if id == "" {
		return nil, errors.New("activity: empty id")
	}
	if name == "" {
		return nil, errors.New("activity: empty name")
	}
	if productID == "" {
		return nil, errors.New("activity: empty product_id")
	}
	if totalStock <= 0 {
		return nil, errors.New("activity: total_stock must be > 0")
	}
	if !end.After(start) {
		return nil, errors.New("activity: end_time must be after start_time")
	}
	return &Activity{
		ID:         id,
		Name:       name,
		ProductID:  productID,
		TotalStock: totalStock,
		StartTime:  start,
		EndTime:    end,
		Status:     StatusDraft,
		WarmupDone: false,
	}, nil
}

// MarkWarmedUp 把活动推进到 active 状态。caller 必须已经把 Redis 写好。
// 幂等:已经 WarmupDone 的直接返回 nil(同 activity_id 重复 warmup 是 no-op)。
// 状态约束:只允许 draft / scheduled → active;ended / cancelled 拒绝。
func (a *Activity) MarkWarmedUp() error {
	if a.WarmupDone {
		return nil // idempotent
	}
	if a.Status != StatusDraft && a.Status != StatusScheduled {
		return &StatusError{Current: a.Status, Wanted: "warmup"}
	}
	a.Status = StatusActive
	a.WarmupDone = true
	return nil
}

// IsLive 表示活动当前接受下单。warmup 完 + 在时段内。
func (a *Activity) IsLive(now time.Time) bool {
	if a.Status != StatusActive {
		return false
	}
	if now.Before(a.StartTime) || now.After(a.EndTime) {
		return false
	}
	return true
}

// StatusError 表示在错误的状态下尝试转移。
type StatusError struct {
	Current Status
	Wanted  string
}

func (e *StatusError) Error() string {
	return "activity: cannot " + e.Wanted + " from status " + string(e.Current)
}

// NotFoundError 表示按 ID 查询不到。
type NotFoundError struct {
	ID string
}

func (e *NotFoundError) Error() string {
	return "activity: not found id=" + e.ID
}
