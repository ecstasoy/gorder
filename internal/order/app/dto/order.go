package dto

import (
	"github.com/ecstasoy/gorder/order/entity"
)

type CreateOrderResponse struct {
	CustomerID  string `json:"customer_id"`
	OrderID     string `json:"order_id"`
	RedirectURL string `json:"redirect_url"`
}

type GetOrderResponse struct {
	OrderID     string         `json:"order_id"`
	CustomerID  string         `json:"customer_id"`
	Status      string         `json:"status"`
	PaymentLink string         `json:"payment_link"`
	Items       []*entity.Item `json:"items"`
}
