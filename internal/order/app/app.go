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
	UpdateOrder      command.UpdateOrderHandler      // 仍保留,内部 saga 使用 repo.Update;若未来无 caller 可删
	SetPaymentLink   command.SetPaymentLinkHandler   // ADR-0002: typed transition,取代 UpdateOrder closure 模式
	CancelOrder      command.CancelOrderHandler      // dispatch to cancel saga (ADR-0002)
	ConfirmOrder     command.ConfirmOrderHandler     // dispatch to confirm saga (ADR-0002)
}

type Queries struct {
	GetCustomerOrder query.GetCustomerOrderHandler
}
