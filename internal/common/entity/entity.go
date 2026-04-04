package entity

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*Item
}

func NewOrder(ID string, customerID string, status orderpb.OrderStatus, paymentLink string, items []*Item) *Order {
	return &Order{ID: ID, CustomerID: customerID, Status: status, PaymentLink: paymentLink, Items: items}
}

func NewValidOrder(ID string, customerID string, status orderpb.OrderStatus, paymentLink string, items []*Item) (*Order, error) {
	for _, item := range items {
		if err := item.validate(); err != nil {
			return nil, err
		}
	}
	return NewOrder(ID, customerID, status, paymentLink, items), nil
}

type Item struct {
	ID       string
	Name     string
	Quantity int32
	PriceID  string
}

func (it Item) validate() error {
	//if err := util.AssertNotEmpty(it.ID, it.PriceID, it.Name); err != nil {
	//	return err
	//}
	var invalidFields []string
	if it.ID == "" {
		invalidFields = append(invalidFields, "ID")
	}
	if it.Name == "" {
		invalidFields = append(invalidFields, "Name")
	}
	if it.PriceID == "" {
		invalidFields = append(invalidFields, "PriceID")
	}
	
	if len(invalidFields) > 0 {
		return fmt.Errorf("item = %v invalid, empty fields: %s", it, strings.Join(invalidFields, ", "))
	}
	
	return nil
}

func NewItem(ID string, name string, quantity int32, priceID string) *Item {
	return &Item{ID: ID, Name: name, Quantity: quantity, PriceID: priceID}
}

func NewValidItem(ID string, name string, quantity int32, priceID string) (*Item, error) {
	item := NewItem(ID, name, quantity, priceID)
	if err := item.validate(); err != nil {
		return nil, err
	}
	return item, nil
}

type ItemWithQuantity struct {
	ID       string
	Quantity int32
}

func (iq ItemWithQuantity) validate() error {
	//if err := util.AssertNotEmpty(iq.ID, iq.Quantity); err != nil {
	//	return err
	//}
	var invalidFields []string
	if iq.ID == "" {
		invalidFields = append(invalidFields, "ID")
	}
	if iq.Quantity < 0 {
		invalidFields = append(invalidFields, "Quantity")
	}
	if len(invalidFields) > 0 {
		return errors.New(strings.Join(invalidFields, ","))
	}
	return nil
}

func NewItemWithQuantity(ID string, quantity int32) *ItemWithQuantity {
	return &ItemWithQuantity{ID: ID, Quantity: quantity}
}

func NewValidItemWithQuantity(ID string, quantity int32) (*ItemWithQuantity, error) {
	iq := NewItemWithQuantity(ID, quantity)
	if err := iq.validate(); err != nil {
		return nil, err
	}
	return iq, nil
}
