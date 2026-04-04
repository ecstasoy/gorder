package entity

import (
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*Item
}

type Item struct {
	ID       string
	Name     string
	Quantity int32
	PriceID  string
}

type ItemWithQuantity struct {
	ID       string
	Quantity int32
}
