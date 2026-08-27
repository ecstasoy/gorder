package domain

import (
	"context"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Processor interface {
	CreatePaymentLink(context.Context, *entity.Order) (string, error)
	// Refund 把 idempotencyKey 当作幂等键 (Stripe 实现传给 Idempotency-Key
	// header)。同一个 key 重复调,Stripe 返回同一个 Refund 对象,不重复扣账。
	// caller (RefundPayment handler) 用 order.refund 消息的 EventID (= outbox
	// 写入时的 uuid) 作为 key —— RabbitMQ 重投或 HandleRetry 重发同一条消息
	// 都用同一个 EventID,Stripe 端自然去重。
	//
	// metadata 会被 attach 到 Refund 对象上 —— charge.refunded webhook 收到时
	// 通过 Refund.Metadata 读回 orderID / customerID,不需要反向 GET PaymentIntent。
	Refund(ctx context.Context, paymentIntentID, idempotencyKey string, metadata map[string]string) error
}

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*entity.Item
}
