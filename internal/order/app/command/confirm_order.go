package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/order/app/intake"
	"github.com/sirupsen/logrus"
)

type ConfirmOrder struct {
	OrderID         string
	CustomerID      string
	PaymentIntentID string // refund 路径必需; saga 在 conflict 时用它构造 outbox payload
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

// Handle ADR-0002 + 2026-06 refund hardening:
//   - 正常路径: saga 在 Mongo tx 里 MarkPaid,然后 stock.Confirm
//   - StatusConflict 路径: saga 在**同一个** Mongo tx 内 outbox.Append refund 请求,
//     再把 conflict err 冒泡上来。consumer 据此 ack (saga 已经把 refund 推到 outbox)。
func (h confirmOrderHandler) Handle(ctx context.Context, cmd ConfirmOrder) (*ConfirmOrderResult, error) {
	if err := h.confirm.Confirm(ctx, intake.ConfirmInput{
		OrderID:         cmd.OrderID,
		CustomerID:      cmd.CustomerID,
		PaymentIntentID: cmd.PaymentIntentID,
	}); err != nil {
		return nil, err
	}
	return &ConfirmOrderResult{}, nil
}
