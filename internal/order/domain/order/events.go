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

// OrderRefundRequestedEvent 表示一个 CANCELLED Order 需要退款 —— 典型场景是
// 支付超时取消后 Stripe webhook 才到达 (29:59 付款, 30:00 超时)。ConfirmOrder
// saga 在 Mongo tx 内检测到 status 冲突 (CANCELLED → PAID 非法) 时 append 此
// 事件,outbox.Worker 异步 publish 到 order.refund queue,payment 消费触发
// Stripe Refund。
//
// 引入这条事件的目的是把"决定要退款"和"发出退款指令"做原子化 —— 之前
// (ADR-0002 之前) 是 consumer 内联 publish,如果 RabbitMQ 临时不可达,事件丢,
// 用户钱被扣但 gorder 不会退,直接金额损失。
type OrderRefundRequestedEvent struct {
	Order           *Order
	PaymentIntentID string // 来自 order.paid 消息体,Stripe Refund 必需
}

func (e OrderRefundRequestedEvent) EventType() string { return "OrderRefundRequested" }

// OrderRefundedEvent 表示 Stripe 已经完成退款 —— 由 payment 服务消费 Stripe
// charge.refunded webhook 后通过 order.refunded queue 通知 order 服务。order
// 消费此事件后 Mongo Tx { o.MarkRefunded(refundID) },把 refund 时间戳 + Stripe
// refund_id 写回 Order。
//
// 注意:这条事件不改 Order.Status (依然是 CANCELLED),只填充 RefundID / RefundedAt
// 两个字段。"已 cancelled + 已 refunded" 是 CANCELLED 的一个子状态,业务上由
// RefundID 是否非空区分,无需新增 Status enum。
type OrderRefundedEvent struct {
	Order    *Order
	RefundID string // Stripe 返回的 re_xxxxx
}

func (e OrderRefundedEvent) EventType() string { return "OrderRefunded" }
