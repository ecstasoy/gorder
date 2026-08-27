package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// MarkRefunded 是 typed command,处理 payment 服务转发的 Stripe charge.refunded
// webhook。它把 RefundID + RefundedAt 写回 Mongo Order(不改 Status,Order 依然
// 是 CANCELLED;refund 只是 CANCELLED 的子状态)。
//
// 幂等:Order.MarkRefunded() 内部检查 RefundID 是否已经非空,已退款时静默跳过。
// 这意味着 Stripe webhook 重发或 RabbitMQ retry 都不会重复写。
type MarkRefunded struct {
	OrderID    string
	CustomerID string
	RefundID   string
	RefundedAt int64
}

type MarkRefundedResult struct{}

type MarkRefundedHandler decorator.CommandHandler[MarkRefunded, *MarkRefundedResult]

type markRefundedHandler struct {
	orderRepo domain.Repository
}

func NewMarkRefundedHandler(
	orderRepo domain.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) MarkRefundedHandler {
	if orderRepo == nil {
		panic("nil orderRepo")
	}
	return decorator.ApplyCommandDecorators[MarkRefunded, *MarkRefundedResult](
		markRefundedHandler{orderRepo: orderRepo},
		logger,
		metricsClient,
	)
}

func (h markRefundedHandler) Handle(ctx context.Context, cmd MarkRefunded) (*MarkRefundedResult, error) {
	if cmd.OrderID == "" {
		return nil, errors.New("MarkRefunded: empty order id")
	}
	if cmd.RefundID == "" {
		return nil, errors.New("MarkRefunded: empty refund id")
	}

	err := h.orderRepo.Update(ctx,
		&domain.Order{ID: cmd.OrderID, CustomerID: cmd.CustomerID},
		func(_ context.Context, oldOrder *domain.Order) (*domain.Order, error) {
			if err := oldOrder.MarkRefunded(cmd.RefundID, cmd.RefundedAt); err != nil {
				return nil, errors.Wrap(err, "MarkRefunded: domain")
			}
			return oldOrder, nil
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "MarkRefunded: mongo update")
	}
	return &MarkRefundedResult{}, nil
}
