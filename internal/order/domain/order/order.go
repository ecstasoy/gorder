package order

import (
	"errors"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*orderpb.Item
}

func NewOrder(id, customerID, status, paymentLink string, items []*orderpb.Item) (*Order, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if customerID == "" {
		return nil, errors.New("customerID is required")
	}
	if status == "" {
		return nil, errors.New("status is required")
	}
	if items == nil {
		return nil, errors.New("items is required")
	}
	return &Order{
		ID:          id,
		CustomerID:  customerID,
		Status:      orderpb.OrderStatus(orderpb.OrderStatus_value[status]),
		PaymentLink: paymentLink,
		Items:       items,
	}, nil
}

func (o *Order) ToProto() *orderpb.Order {
	return &orderpb.Order{
		ID:          o.ID,
		CustomerID:  o.CustomerID,
		Status:      o.Status,
		PaymentLink: o.PaymentLink,
		Items:       o.Items,
	}
}
