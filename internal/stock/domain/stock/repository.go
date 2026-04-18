package stock

import (
	"context"
	"fmt"
	"strings"

	"github.com/ecstasoy/gorder/common/entity"
)

type Repository interface {
	GetStock(ctx context.Context, ids []string) ([]*entity.ItemWithQuantity, error)
	UpdateStock(
		ctx context.Context,
		data []*entity.ItemWithQuantity,
		updateFunc func(
			ctx context.Context,
			existing []*entity.ItemWithQuantity,
			query []*entity.ItemWithQuantity,
		) ([]*entity.ItemWithQuantity, error),
	) error
	RestoreStock(ctx context.Context, items []*entity.ItemWithQuantity) error
	DeductStock(ctx context.Context, items []*entity.ItemWithQuantity) error
	// UpsertStock 把指定 product_id 的 quantity SET 为给定值(不是增量)。
	// 用于秒杀 warmup —— 每次活动开始时把 flash SKU 的库存重置为本场活动的总量。
	UpsertStock(ctx context.Context, items []*entity.ItemWithQuantity) error
}

type NotFoundError struct {
	Missing []string
}

func (e NotFoundError) Error() string {
	return "items not found: " + strings.Join(e.Missing, ", ")
}

type ExceedStockError struct {
	FailedOn []struct {
		ID   string
		Want int32
		Have int32
	}
}

func (e ExceedStockError) Error() string {
	var info []string
	for _, v := range e.FailedOn {
		info = append(info, fmt.Sprintf("product_id=%s, want %d, have %d", v.ID, v.Want, v.Have))
	}
	return fmt.Sprintf("not enough stock for [%s]", strings.Join(info, ","))
}
