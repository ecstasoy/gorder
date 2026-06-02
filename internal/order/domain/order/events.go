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
// (延迟队列 DLX) 或者主动取消。ADR-0002 把它接入 outbox(通过 CancelOrder
// saga 的 PullEvents),所以 cancel 路径现在和 intake 同形。
type OrderCancelledEvent struct {
	Order *Order
}

func (e OrderCancelledEvent) EventType() string { return "OrderCancelled" }

// OrderPaidEvent 表示一个 Order 被标为 PAID (ADR-0002 引入)。
// 由 ConfirmOrder saga 在 Mongo tx 里 record,通过 outbox 异步发布到 RabbitMQ
// 替代 ADR-0001 时 payment 服务直接 publish 的 dual-write 路径。
type OrderPaidEvent struct {
	Order *Order
}

func (e OrderPaidEvent) EventType() string { return "OrderPaid" }
