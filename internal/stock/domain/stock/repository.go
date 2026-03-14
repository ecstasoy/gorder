package stock

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Repository interface {
	GetItems(ctx context.Context, ids []string) ([]*orderpb.Item, error)
}

type NotFoundError struct {
	Missing []string
}

func (e NotFoundError) Error() string {
	return "items not found: " + string(e.Missing[0])
}
