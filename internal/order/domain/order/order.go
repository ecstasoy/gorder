package order

import (
	"errors"
	"slices"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*entity.Item
}

func NewOrder(id, customerID, status, paymentLink string, items []*entity.Item) (*Order, error) {
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

func (o *Order) UpdatePaymentLink(paymentLink string) error {
	//if paymentLink == "" {
	//	return errors.New("cannot update empty paymentLink")
	//}
	o.PaymentLink = paymentLink
	return nil
}

func (o *Order) UpdateItems(items []*entity.Item) error {
	o.Items = items
	return nil
}

func (o *Order) UpdateStatus(to orderpb.OrderStatus) error {
	if !o.isValidStatusTransition(to) {
		return &StatusConflictError{
			OrderID:       o.ID,
			CurrentStatus: o.Status,
			TargetStatus:  to,
		}
	}
	o.Status = to
	return nil
}

func (o *Order) isValidStatusTransition(to orderpb.OrderStatus) bool {
	if o.Status == to {
		return true
	}

	validTransitions := map[orderpb.OrderStatus][]orderpb.OrderStatus{
		orderpb.OrderStatus_ORDER_STATUS_PENDING: {
			orderpb.OrderStatus_ORDER_STATUS_PAID,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_PAID: {
			orderpb.OrderStatus_ORDER_STATUS_PREPARING,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_PREPARING: {
			orderpb.OrderStatus_ORDER_STATUS_READY,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_READY: {
			orderpb.OrderStatus_ORDER_STATUS_DELIVERING,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_DELIVERING: {
			orderpb.OrderStatus_ORDER_STATUS_DELIVERED,
		},
	}

	allowedStatuses, ok := validTransitions[o.Status]
	if !ok {
		return false
	}

	return slices.Contains(allowedStatuses, to)
}
