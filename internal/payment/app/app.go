package app

import (
	"github.com/ecstasoy/gorder/payment/app/command"
)

type Application struct {
	Commands Commands
}

type Commands struct {
	CreatePayment command.CreatePaymentHandler
	RefundPayment command.RefundPaymentHandler
}
