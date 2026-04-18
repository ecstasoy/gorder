package app

import (
	"github.com/ecstasoy/gorder/order/app/command"
	"github.com/ecstasoy/gorder/order/app/query"
)

type Application struct {
	Commands Commands
	Queries  Queries
}

type Commands struct {
	CreateOrder      command.CreateOrderHandler
	CreateFlashOrder command.CreateFlashOrderHandler
	UpdateOrder      command.UpdateOrderHandler
	CancelOrder      command.CancelOrderHandler
}

type Queries struct {
	GetCustomerOrder query.GetCustomerOrderHandler
}
