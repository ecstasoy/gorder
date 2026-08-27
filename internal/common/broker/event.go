package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/pkg/errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
)

// amqp091 *amqp.Channel 不是 goroutine-safe —— 多个 goroutine 并发调用
// Channel.Publish 会交错写入 TCP 帧,被 broker 检测到后 close channel。
// 旧做法: 1 个 channel + 全局 mutex → publish 串行,吞吐上限约 2-3k msg/s。
// 新做法: N 个 channel,每个自己一把 mutex,round-robin 分配 → 吞吐随 N 线性扩展,
type pooledChannel struct {
	ch *amqp.Channel
	mu sync.Mutex
}

type channelPool struct {
	channels []*pooledChannel
	next     atomic.Uint64
}

func newChannelPool(conn *amqp.Connection, size int) (*channelPool, error) {
	if size <= 0 {
		size = 8
	}
	pcs := make([]*pooledChannel, 0, size)
	for i := 0; i < size; i++ {
		ch, err := conn.Channel()
		if err != nil {
			for _, pc := range pcs {
				_ = pc.ch.Close()
			}
			return nil, fmt.Errorf("channel pool: open channel #%d: %w", i, err)
		}
		pcs = append(pcs, &pooledChannel{ch: ch})
	}
	return &channelPool{channels: pcs}, nil
}

// withChannel pick a channel in round-robin to apply to fn
func (p *channelPool) withChannel(fn func(*amqp.Channel) error) error {
	i := p.next.Add(1) % uint64(len(p.channels))
	pc := p.channels[i]
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return fn(pc.ch)
}

func (p *channelPool) close() {
	for _, pc := range p.channels {
		_ = pc.ch.Close()
	}
}

var pubPool *channelPool

func initPublisherPool(conn *amqp.Connection, size int) error {
	pool, err := newChannelPool(conn, size)
	if err != nil {
		return err
	}
	pubPool = pool
	logrus.Infof("Initialized RabbitMQ publisher pool with %d channels", len(pool.channels))
	return nil
}

const (
	EventOrderCreated        = "order.created"
	EventOrderPaid           = "order.paid"
	EventOrderCancelled      = "order.cancelled" // ADR-0002: 加入 outbox 后,发到 direct queue,等下游订阅
	EventOrderPaymentTimeout = "order.payment.timeout"
	OrderPaymentDelayQueue   = "order.payment.delay"
	OrderPaymentTimeoutDLX   = "order.payment.timeout.dlx"
	EventOrderRefund         = "order.refund"
	EventOrderRefunded       = "order.refunded"
	EventFlashSaleOrder      = "flash.order.created"
)

// OrderPaidEvent 在原有 Order 字段基础上携带 PaymentIntentID，
// 用于在状态冲突时发起退款。JSON 字段名与 entity.Order 保持一致，
// 不破坏 kitchen/order consumer 的现有反序列化。
type OrderPaidEvent struct {
	ID              string              `json:"ID"`
	CustomerID      string              `json:"CustomerID"`
	Status          orderpb.OrderStatus `json:"Status"`
	PaymentLink     string              `json:"PaymentLink"`
	Items           []*entity.Item      `json:"Items"`
	PaymentIntentID string              `json:"PaymentIntentID,omitempty"`
}

// OrderRefundPayload 是退款事件的消息体。
//
// EventID 是 outbox.Record.EventID 的副本,payment 服务消费时把它当成 Stripe
// Idempotency-Key 传给 stripeRefund.New —— 即使 RabbitMQ 重投同一条消息或
// HandleRetry 重发,Stripe 都会返回**同一个** Refund 对象,不会重复扣账户余额。
type OrderRefundPayload struct {
	OrderID         string `json:"OrderID"`
	CustomerID      string `json:"CustomerID"`
	PaymentIntentID string `json:"PaymentIntentID"`
	EventID         string `json:"EventID"`
}

// OrderRefundedPayload 是 payment 服务转发 Stripe charge.refunded webhook 给
// order 服务的消息体。order 消费后 MarkRefunded 把 refund_id 写回 Mongo。
type OrderRefundedPayload struct {
	OrderID    string `json:"OrderID"`
	CustomerID string `json:"CustomerID"`
	RefundID   string `json:"RefundID"`   // Stripe re_xxxxx
	RefundedAt int64  `json:"RefundedAt"` // unix seconds
}

type FlashSaleOrderPayload struct {
	Token      string          `json:"token"`
	CustomerID string          `json:"customer_id"`
	ActivityID string          `json:"activity_id,omitempty"` // ADR-0004,旧消息向后兼容时可空
	Items      []FlashSaleItem `json:"items"`
}

type FlashSaleItem struct {
	ItemID   string `json:"item_id"`
	Quantity int32  `json:"quantity"`
}

type RoutingType string

const (
	FanOut RoutingType = "fan-out"
	Direct RoutingType = "direct"
)

type PublishEventReq struct {
	Channel  *amqp.Channel
	Routing  RoutingType
	Queue    string
	Exchange string
	Body     any
}

func publishEvent(ctx context.Context, p PublishEventReq) (err error) {
	_, dLog := logging.WhenEventPublish(ctx, p)
	defer dLog(nil, &err)

	if err = checkParam(p); err != nil {
		return err
	}

	switch p.Routing {
	default:
		logrus.WithContext(ctx).Panicf("unsupported routing type: %s", string(p.Routing))
	case FanOut:
		return fanOut(ctx, p)
	case Direct:
		return directQueue(ctx, p)
	}
	return nil
}

func checkParam(p PublishEventReq) error {
	if p.Channel == nil {
		return errors.New("nil channel")
	}
	return nil
}

func directQueue(ctx context.Context, p PublishEventReq) (err error) {
	// QueueDeclare 已经在 broker.Connect() 启动时做过,这里不再做 —
	// QueueDeclare 不是 thread-safe,如果放在 hot path 上,
	// 500 个并发 goroutine 会把 channel 打坏。
	jsonBody, err := json.Marshal(p.Body)
	if err != nil {
		return err
	}
	return doPublish(ctx, p.Channel, p.Exchange, p.Queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         jsonBody,
		Headers:      InjectRabbitMQHeaders(ctx),
	})
}

func publishToDelayQueue(ctx context.Context, ch *amqp.Channel, body any) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return doPublish(ctx, ch, "", OrderPaymentDelayQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         jsonBody,
		Headers:      InjectRabbitMQHeaders(ctx),
	})
}

func fanOut(ctx context.Context, p PublishEventReq) (err error) {
	jsonBody, err := json.Marshal(p.Body)
	if err != nil {
		return err
	}
	return doPublish(ctx, p.Channel, p.Exchange, "", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         jsonBody,
		Headers:      InjectRabbitMQHeaders(ctx),
	})
}

func doPublish(ctx context.Context, _ *amqp.Channel, exchange, key string, mandatory bool, immediate bool, msg amqp.Publishing) error {
	if pubPool == nil {
		return errors.New("publisher pool not initialized; call broker.Connect first")
	}

	return pubPool.withChannel(func(ch *amqp.Channel) error {
		if err := ch.PublishWithContext(ctx, exchange, key, mandatory, immediate, msg); err != nil {
			logging.Warnf(ctx, nil, "_publish_event_failed || exchange=%s || key=%s || msg=%v", exchange, key, msg)
			return errors.Wrap(err, "publish event error")
		}
		return nil
	})
}
