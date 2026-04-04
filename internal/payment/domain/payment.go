package domain

import (
	"context"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Processor interface {
	CreatePaymentLink(context.Context, *entity.Order) (string, error)
	Refund(ctx context.Context, paymentIntentID string) error
}

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*entity.Item
}
