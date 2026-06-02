package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/sirupsen/logrus"
)

// SetPaymentLink 是 ADR-0002 引入的 typed command,取代 UpdateOrder 的 closure
// 模式。Payment 服务在 Stripe 创建 checkout session 后,通过此 command 把
// payment_link 写入 Order。
//
// 状态机规则:只有 PENDING Order 可以被设置 payment_link。已 CANCELLED 的订单
// 跳过(场景:webhook 慢到 Order 已 timeout)——这个边界 if 现在是 command 的
// 内部逻辑,而不是 caller 写在 closure 里。
type SetPaymentLink struct {
	OrderID     string
	CustomerID  string
	PaymentLink string
	Items       []*domain.Order // 旧契约里 Payment 服务也回传 items;此处保留以避免 API 不兼容
}

type SetPaymentLinkHandler decorator.CommandHandler[SetPaymentLink, struct{}]

type setPaymentLinkHandler struct {
	orderRepo domain.Repository
}

func NewSetPaymentLinkHandler(
	orderRepo domain.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) SetPaymentLinkHandler {
	if orderRepo == nil {
		panic("nil orderRepo")
	}
	return decorator.ApplyCommandDecorators[SetPaymentLink, struct{}](
		setPaymentLinkHandler{orderRepo: orderRepo},
		logger,
		metricsClient,
	)
}

func (h setPaymentLinkHandler) Handle(ctx context.Context, cmd SetPaymentLink) (struct{}, error) {
	var err error
	defer logging.WhenCommandExecute(ctx, "SetPaymentLinkHandler.Handle", cmd.OrderID, err)

	err = h.orderRepo.Update(ctx,
		&domain.Order{ID: cmd.OrderID, CustomerID: cmd.CustomerID},
		func(sCtx context.Context, oldOrder *domain.Order) (*domain.Order, error) {
			// 边界:已 CANCELLED 的订单,Stripe webhook 慢到 → 这里幂等跳过。
			// 这个规则原本在 caller (ports/grpc.go) 的 closure 里;ADR-0002 把它
			// 内化为 command 的语义。
			if oldOrder.Status == orderpb.OrderStatus_ORDER_STATUS_CANCELLED {
				logrus.WithContext(sCtx).Warnf("SetPaymentLink: skipping stale write for cancelled order %s", oldOrder.ID)
				return oldOrder, nil
			}
			if updateErr := oldOrder.UpdatePaymentLink(cmd.PaymentLink); updateErr != nil {
				return nil, updateErr
			}
			return oldOrder, nil
		},
	)
	return struct{}{}, err
}
