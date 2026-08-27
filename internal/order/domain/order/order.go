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
