package order

import "github.com/ecstasoy/gorder/common/genproto/orderpb"

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*orderpb.Item
}
