package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type OrderService interface {
	UpdateOrder(ctx context.Context, order *orderpb.Order) error
}
