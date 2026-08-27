package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/tracing"
	"github.com/spf13/viper"
	"github.com/stripe/stripe-go/v80"
	"github.com/stripe/stripe-go/v80/checkout/session"
	stripRefund "github.com/stripe/stripe-go/v80/refund"
)

type StripeProcessor struct {
	apiKey string
}

func NewStripeProcessor(apiKey string) *StripeProcessor {
	if apiKey == "" {
		panic("stripe api key is required")
	}
	stripe.Key = apiKey
	return &StripeProcessor{
		apiKey: apiKey,
	}
}

var (
	successURL = "http://localhost:9090/payment/success"
)

func (s StripeProcessor) CreatePaymentLink(ctx context.Context, order *entity.Order) (string, error) {
	_, span := tracing.Start(ctx, "stripe_processor.create_payment_link")
	defer span.End()

	var items []*stripe.CheckoutSessionLineItemParams
	for _, item := range order.Items {
		items = append(items, &stripe.CheckoutSessionLineItemParams{
			Price:    stripe.String(item.PriceID),
			Quantity: stripe.Int64(int64(item.Quantity)),
		})
	}

	marshalledItems, _ := json.Marshal(order.Items)
	metadata := map[string]string{
		"orderID":     order.ID,
		"customerID":  order.CustomerID,
		"status":      string(order.Status),
		"items":       string(marshalledItems),
		"paymentLink": order.PaymentLink,
	}
	timeoutMs := viper.GetInt64("rabbitmq.payment-timeout-ms")
	if timeoutMs == 0 {
		timeoutMs = 900000
	}
	// Stripe 最小过期时间为 30 分钟
	expiresAt := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	if time.Until(expiresAt) < 30*time.Minute {
		expiresAt = time.Now().Add(30 * time.Minute)
	}

	params := &stripe.CheckoutSessionParams{
		Metadata:   metadata,
		LineItems:  items,
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(fmt.Sprintf("%s?customerID=%s&orderID=%s", successURL, order.CustomerID, order.ID)),
		ExpiresAt:  stripe.Int64(expiresAt.Unix()),
	}
	result, err := session.New(params)
	if err != nil {
		return "", err
	}
	return result.URL, nil
}

func (s StripeProcessor) Refund(ctx context.Context, paymentIntentID, idempotencyKey string, metadata map[string]string) error {
	_, span := tracing.Start(ctx, "stripe_processor.refund")
	defer span.End()

	params := &stripe.RefundParams{
		PaymentIntent: stripe.String(paymentIntentID),
	}
	// Metadata: orderID/customerID。charge.refunded webhook 收到时通过
	// Refund.Metadata 读回,避免反向 GET PaymentIntent。
	for k, v := range metadata {
		params.AddMetadata(k, v)
	}
	// Idempotency-Key 防止 RabbitMQ retry / HandleRetry 重发导致多次 Stripe 扣账。
	// 同 key 重复请求 Stripe 返回缓存的 Refund 对象,网络层即去重。
	if idempotencyKey != "" {
		params.SetIdempotencyKey(idempotencyKey)
	}

	_, err := stripRefund.New(params)
	return err
}
