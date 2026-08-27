package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/payment/domain"
	"github.com/sirupsen/logrus"
)

type RefundPayment struct {
	OrderID         string
	CustomerID      string
	PaymentIntentID string
	EventID         string // outbox.Record.EventID, 用作 Stripe Idempotency-Key
}

type RefundPaymentHandler decorator.CommandHandler[RefundPayment, struct{}]

type refundPaymentHandler struct {
	processor domain.Processor
}

func NewRefundPaymentHandler(
	processor domain.Processor,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) RefundPaymentHandler {
	return decorator.ApplyCommandDecorators[RefundPayment, struct{}](
		refundPaymentHandler{processor: processor},
		logger,
		metricsClient,
	)
}

func (h refundPaymentHandler) Handle(ctx context.Context, cmd RefundPayment) (struct{}, error) {
	if cmd.PaymentIntentID == "" {
		logrus.WithContext(ctx).Warnf("refund skipped: empty PaymentIntentID, orderID=%s", cmd.OrderID)
		return struct{}{}, nil
	}
	// EventID 作 Stripe Idempotency-Key —— RabbitMQ 重投 / HandleRetry 重发同一
	// 条 order.refund 消息时,Stripe 端会返回缓存的 Refund 对象,不重复扣账。
	// 空 EventID 时不传 (旧消息兼容);Stripe 行为退化为"按 PaymentIntent 自身
	// 去重"——同 PI 已有 refund 时 Stripe 返回 error,我们记日志即可。
	//
	// Metadata 让 charge.refunded webhook 能通过 Refund.Metadata 反查 orderID,
	// 不需要再调 Stripe 一次 GET PaymentIntent。
	return struct{}{}, h.processor.Refund(ctx, cmd.PaymentIntentID, cmd.EventID, map[string]string{
		"orderID":    cmd.OrderID,
		"customerID": cmd.CustomerID,
	})
}
