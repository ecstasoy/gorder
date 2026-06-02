package order

// DomainEvent 是 Order aggregate 在状态转移时产生的事件,由 saga 在 PullEvents
// 后翻译到 outbox。aggregate 不知道 RabbitMQ / 队列拓扑 / 序列化形式 —— 这些
// 是 saga 这一侧的责任 (见 ADR-0001 Step 6)。
type DomainEvent interface {
	EventType() string
}

// OrderCreatedEvent 表示一个新的 pending Order 被收单。saga 把它翻译为
// `order.created` 的 outbox 记录;下游 (payment / kitchen) 消费它启动支付
// 链路。事件持有 *Order ref —— Repo.Create 在事务里给 Order 分配 ID 之后,
// saga PullEvents 时 ref 已经指向已分配 ID 的 Order。
type OrderCreatedEvent struct {
	Order *Order
}

func (e OrderCreatedEvent) EventType() string { return "OrderCreated" }

// OrderCancelledEvent 表示一个 pending Order 被取消 —— 来自支付超时
// (延迟队列 DLX) 或者主动取消。Step 7 暂未给它接 outbox,目前还没有下游消费
// `order.cancelled`;聚合自己产生事件、cancel handler 不 PullEvents,事件随
// aggregate ref 一起被 GC。未来需要广播取消时,handler 像 intake saga 一样
// PullEvents + translate 即可。
type OrderCancelledEvent struct {
	Order *Order
}

func (e OrderCancelledEvent) EventType() string { return "OrderCancelled" }
