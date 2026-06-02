package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/order/app/intake"
	"github.com/sirupsen/logrus"
)

type CancelOrder struct {
	OrderID    string
	CustomerID string
}

type CancelOrderResult struct{}

type CancelOrderHandler decorator.CommandHandler[CancelOrder, *CancelOrderResult]

type cancelOrderHandler struct {
	cancel intake.CancelOrder
}

func NewCancelOrderHandler(
	cancel intake.CancelOrder,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CancelOrderHandler {
	if cancel == nil {
		panic("nil cancel saga")
	}
	return decorator.ApplyCommandDecorators[CancelOrder, *CancelOrderResult](
		cancelOrderHandler{cancel: cancel},
		logger,
		metricsClient,
	)
}

// Handle 退化为参数转换 + 调 saga。ADR-0002 落地后,Cancel 走 saga module,
// PullEvents 进 outbox,Release 在 tx 后调用。原 handler 的 closure-in-Update
// 写 status + handler 末尾调 stockGRPC.RestoreStock 的形态消失。
func (h cancelOrderHandler) Handle(ctx context.Context, cmd CancelOrder) (*CancelOrderResult, error) {
	_, err := h.cancel.Cancel(ctx, intake.CancelInput{
		OrderID:    cmd.OrderID,
		CustomerID: cmd.CustomerID,
	})
	if err != nil {
		return nil, err
	}
	return &CancelOrderResult{}, nil
}
