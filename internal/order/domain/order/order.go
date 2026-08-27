package order

import (
	"errors"
	"slices"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type Order struct {
	ID          string
	CustomerID  string
	Status      orderpb.OrderStatus
	PaymentLink string
	Items       []*entity.Item

	// RefundID + RefundedAt 是 Order 的 "refund 子状态" (Phase 2026-06)。
	// 设计选择:不新增 ORDER_STATUS_REFUNDED enum,而是用这两个字段表达
	// "CANCELLED 且已退款"。理由:Order.Status 是面向客户的生命周期 (PENDING
	// → PAID → ... 或 CANCELLED), refund 是内部资金状态,两者正交。RefundID
	// 非空 ⟺ 已退款。
	RefundID   string  // Stripe 返回的 re_xxxxx
	RefundedAt *int64  // unix seconds, 用指针区分 "未退款" 和 "1970"

	// ActivityID (ADR-0004) — 如果订单是 flash sale 活动产生的,记录活动 ID。
	// 常规下单时为空字符串。用途:运营报表 / 按活动取消 / A/B 测试归属。
	ActivityID string

	// events 是 aggregate 在状态转移时记录的 domain event,不参与持久化也不
	// 参与序列化 (unexported field — encoding/json + bson driver 都会跳过)。
	// saga 用 PullEvents() 拿走后清空。
	events []DomainEvent
}

func NewOrder(id, customerID, status, paymentLink string, items []*entity.Item) (*Order, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if customerID == "" {
		return nil, errors.New("customerID is required")
	}
	if status == "" {
		return nil, errors.New("status is required")
	}
	if items == nil {
		return nil, errors.New("items is required")
	}
	return &Order{
		ID:          id,
		CustomerID:  customerID,
		Status:      orderpb.OrderStatus(orderpb.OrderStatus_value[status]),
		PaymentLink: paymentLink,
		Items:       items,
	}, nil
}

func NewPendingOrder(customerId string, items []*entity.Item) (*Order, error) {
	if customerId == "" {
		return nil, errors.New("empty customerID")
	}
	if items == nil {
		return nil, errors.New("empty items")
	}
	o := &Order{
		CustomerID: customerId,
		Status:     orderpb.OrderStatus_ORDER_STATUS_PENDING,
		Items:      items,
	}
	// Capture *Order — Repo.Create 会在同一 ref 上 set ID,后续 PullEvents
	// 拿到的 OrderCreatedEvent.Order.ID 已经是分配后的值。
	o.events = append(o.events, OrderCreatedEvent{Order: o})
	return o, nil
}

// PullEvents 返回自上次 pull 以来记录的 domain event,并清空内部 list。
// 调用者 (saga) 拿到后翻译成 outbox 记录;再次 pull 不会重复拿到同一组事件。
func (o *Order) PullEvents() []DomainEvent {
	if len(o.events) == 0 {
		return nil
	}
	events := o.events
	o.events = nil
	return events
}

func (o *Order) UpdatePaymentLink(paymentLink string) error {
	//if paymentLink == "" {
	//	return errors.New("cannot update empty paymentLink")
	//}
	o.PaymentLink = paymentLink
	return nil
}

func (o *Order) UpdateItems(items []*entity.Item) error {
	o.Items = items
	return nil
}

func (o *Order) UpdateStatus(to orderpb.OrderStatus) error {
	if !o.isValidStatusTransition(to) {
		return &StatusConflictError{
			OrderID:       o.ID,
			CurrentStatus: o.Status,
			TargetStatus:  to,
		}
	}
	o.Status = to
	return nil
}

// Cancel 把 Order 状态推到 CANCELLED 并记录 OrderCancelledEvent。
// 与 UpdateStatus 不同的是 Cancel 是一个具名的领域动作 —— cancel handler
// 调用它而不是手写 UpdateStatus,让"取消"这个意图显式地住在 aggregate
// 上,自然 append 事件 (ADR-0001 Step 3 模式),不会有人忘了记录。
func (o *Order) Cancel() error {
	if err := o.UpdateStatus(orderpb.OrderStatus_ORDER_STATUS_CANCELLED); err != nil {
		return err
	}
	o.events = append(o.events, OrderCancelledEvent{Order: o})
	return nil
}

// MarkPaid 把 Order 状态推到 PAID 并记录 OrderPaidEvent。同 Cancel 模式:
// ADR-0002 引入,让 "支付成功" 成为具名领域动作。caller (ConfirmOrder saga)
// 调它而不是手写 UpdateStatus(PAID)。
func (o *Order) MarkPaid() error {
	if err := o.UpdateStatus(orderpb.OrderStatus_ORDER_STATUS_PAID); err != nil {
		return err
	}
	o.events = append(o.events, OrderPaidEvent{Order: o})
	return nil
}

// RequestRefund 记录一个 OrderRefundRequestedEvent (不改 Status — Order 已是
// CANCELLED)。caller (ConfirmOrder saga 在 StatusConflictError 路径上) 在 Mongo
// tx 内调,然后 PullEvents → outbox.Append → worker 异步 publish 到 order.refund。
//
// 参数 paymentIntentID 来自 order.paid 消息体 (payment webhook 转发)。Stripe
// Refund 必需。
func (o *Order) RequestRefund(paymentIntentID string) error {
	if paymentIntentID == "" {
		return errors.New("RequestRefund: empty paymentIntentID")
	}
	o.events = append(o.events, OrderRefundRequestedEvent{
		Order:           o,
		PaymentIntentID: paymentIntentID,
	})
	return nil
}

// MarkRefunded 写入 Stripe 退款已完成的事实。不改 Status (Order 已 CANCELLED)。
// 由 order 消费 order.refunded 事件 (payment 转发 Stripe charge.refunded webhook)
// 时调用。幂等:已 refunded 时静默跳过,避免 Stripe webhook 重发导致 OrderRefundedEvent
// 重复 append。
func (o *Order) MarkRefunded(refundID string, refundedAt int64) error {
	if refundID == "" {
		return errors.New("MarkRefunded: empty refundID")
	}
	if o.RefundID != "" {
		// 幂等:已退过,无声跳过。worker 重投或 Stripe webhook 重发时安全。
		return nil
	}
	o.RefundID = refundID
	o.RefundedAt = &refundedAt
	o.events = append(o.events, OrderRefundedEvent{Order: o, RefundID: refundID})
	return nil
}

func (o *Order) isValidStatusTransition(to orderpb.OrderStatus) bool {
	if o.Status == to {
		return true
	}

	validTransitions := map[orderpb.OrderStatus][]orderpb.OrderStatus{
		orderpb.OrderStatus_ORDER_STATUS_PENDING: {
			orderpb.OrderStatus_ORDER_STATUS_PAID,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_PAID: {
			orderpb.OrderStatus_ORDER_STATUS_PREPARING,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_PREPARING: {
			orderpb.OrderStatus_ORDER_STATUS_READY,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_READY: {
			orderpb.OrderStatus_ORDER_STATUS_DELIVERING,
			orderpb.OrderStatus_ORDER_STATUS_CANCELLED,
		},
		orderpb.OrderStatus_ORDER_STATUS_DELIVERING: {
			orderpb.OrderStatus_ORDER_STATUS_DELIVERED,
		},
	}

	allowedStatuses, ok := validTransitions[o.Status]
	if !ok {
		return false
	}

	return slices.Contains(allowedStatuses, to)
}
