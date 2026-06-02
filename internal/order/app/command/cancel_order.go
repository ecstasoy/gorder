package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/order/app/query"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/sirupsen/logrus"
)

type CancelOrder struct {
	OrderID    string
	CustomerID string
}

type CancelOrderResult struct{}

type CancelOrderHandler decorator.CommandHandler[CancelOrder, *CancelOrderResult]

type cancelOrderHandler struct {
	orderRepo domain.Repository
	stockGRPC query.StockService
}

func NewCancelOrderHandler(
	orderRepo domain.Repository,
	stockGRPC query.StockService,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CancelOrderHandler {
	if orderRepo == nil {
		panic("orderRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[CancelOrder, *CancelOrderResult](
		cancelOrderHandler{
			orderRepo: orderRepo,
			stockGRPC: stockGRPC,
		},
		logger,
		metricsClient,
	)
}

// Handle 按 ADR-0001 Step 7 改造:
//   - 状态转移走 o.Cancel() (具名领域动作 + append OrderCancelledEvent),
//     而不是手写 UpdateStatus(CANCELLED)
//   - 库存归还走 stockGRPC.Release(orderID),不再用旧的 RestoreStock(items)。
//     Release 是 reservation 感知的:对幂等的 held → released 转移、对
//     已 confirmed 的 reservation 返回 conflict (不该 release 已支付的单)。
//     不需要 caller 携带 items —— reservation 表自带 (order_id, items)。
func (h cancelOrderHandler) Handle(ctx context.Context, cmd CancelOrder) (*CancelOrderResult, error) {
	var didCancel bool

	err := h.orderRepo.Update(ctx,
		&domain.Order{ID: cmd.OrderID, CustomerID: cmd.CustomerID},
		func(ctx context.Context, o *domain.Order) (*domain.Order, error) {
			if o.Status != orderpb.OrderStatus_ORDER_STATUS_PENDING {
				return o, nil
			}
			if err := o.Cancel(); err != nil {
				return nil, err
			}
			didCancel = true
			return o, nil
		},
	)
	if err != nil {
		return nil, err
	}

	if didCancel {
		if err := h.stockGRPC.Release(ctx, cmd.OrderID); err != nil {
			// 订单已取消,Release 失败只记录日志,不回滚 ——
			// stock 服务的 zombie 扫描会兜底 (ADR-0001 Step 4 设计预留)。
			logrus.WithContext(ctx).Errorf("order %s cancelled but Release failed: %v", cmd.OrderID, err)
		}
	}

	return &CancelOrderResult{}, nil
}
