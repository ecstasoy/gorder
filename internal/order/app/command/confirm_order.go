package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/order/app/intake"
	"github.com/sirupsen/logrus"
)

type ConfirmOrder struct {
	OrderID    string
	CustomerID string
}

type ConfirmOrderResult struct{}

type ConfirmOrderHandler decorator.CommandHandler[ConfirmOrder, *ConfirmOrderResult]

type confirmOrderHandler struct {
	confirm intake.ConfirmOrder
}

func NewConfirmOrderHandler(
	confirm intake.ConfirmOrder,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) ConfirmOrderHandler {
	if confirm == nil {
		panic("nil confirm saga")
	}
	return decorator.ApplyCommandDecorators[ConfirmOrder, *ConfirmOrderResult](
		confirmOrderHandler{confirm: confirm},
		logger,
		metricsClient,
	)
}

// Handle ADR-0002: order.paid consumer 现在 dispatch 到 ConfirmOrder command,
// command 调 saga,saga 在 Mongo tx 里 MarkPaid + outbox.Append,然后 stock.Confirm。
// StatusConflictError 仍然冒泡上来,consumer 据此发 refund event。
func (h confirmOrderHandler) Handle(ctx context.Context, cmd ConfirmOrder) (*ConfirmOrderResult, error) {
	if err := h.confirm.Confirm(ctx, intake.ConfirmInput{
		OrderID:    cmd.OrderID,
		CustomerID: cmd.CustomerID,
	}); err != nil {
		return nil, err
	}
	return &ConfirmOrderResult{}, nil
}
