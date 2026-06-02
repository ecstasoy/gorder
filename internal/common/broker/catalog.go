package broker

import "context"

// EventCatalog 是 publish 端的 typed facade(ADR-0002 candidate 7)。
//
// 目的:把"事件 X 走哪个 routing(direct queue / fanout exchange / TTL queue)"
// 这件事从 caller 收回到一处。caller 调 `cat.PublishOrderRefund(...)`,不再
// 需要知道这是 direct 还是 fanout。topology 知识住在本文件,future 换 Kafka / NATS
// 时只动这里。
//
// 现状:绝大多数 publish 已经走 outbox(outbox.Worker 内部按 Record.Kind 调
// Publisher.Publish / Broadcast / PublishDelayed)。剩下少量直 publish 的
// caller (主要是 refund 路径) 通过这个 catalog 走。
type EventCatalog struct {
	pub Publisher
}

func NewEventCatalog(pub Publisher) *EventCatalog {
	if pub == nil {
		panic("EventCatalog: nil publisher")
	}
	return &EventCatalog{pub: pub}
}

// PublishOrderRefund 发出 order.refund 事件(direct queue, Payment 服务订阅,
// 触发 Stripe Refund API 调用)。
//
// 触发场景:order.paid 消费时发现订单已 CANCELLED,需要退款。
func (c *EventCatalog) PublishOrderRefund(ctx context.Context, payload OrderRefundPayload) error {
	return c.pub.Publish(ctx, DomainEvent{
		Dest: EventOrderRefund,
		Data: payload,
	})
}

// PublishFlashOrderCreated 发出 flash.order.created 事件(direct queue, 闪购消费者订阅,
// 异步建单)。
func (c *EventCatalog) PublishFlashOrderCreated(ctx context.Context, payload FlashSaleOrderPayload) error {
	return c.pub.Publish(ctx, DomainEvent{
		Dest: EventFlashSaleOrder,
		Data: payload,
	})
}
