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
	return struct{}{}, h.processor.Refund(ctx, cmd.PaymentIntentID)
}
