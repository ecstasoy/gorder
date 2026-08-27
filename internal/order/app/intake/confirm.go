package intake

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/order/app/query"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	domainsvc "github.com/ecstasoy/gorder/order/domain/service"
	"github.com/pkg/errors"
)

// ConfirmInput 是 ConfirmOrder.Confirm 的输入。来自 order.paid 消息。
// PaymentIntentID 是 refund 路径必需 —— 如果 saga 在 Mongo tx 内发现 Order
// 已经 CANCELLED,会用它在同一个事务里 outbox.Append(OrderRefundRequestedEvent)。
type ConfirmInput struct {
	OrderID         string
	CustomerID      string
	PaymentIntentID string
}

// ConfirmOrder 是 saga 生命周期的第二段(ADR-0002):order.paid 收到后,
// 把 Order 推到 PAID + 让 stock 服务把 reservation 从 held 转 confirmed。
//
// 镜像 IntakeOrder 的三段式:
//
//	阶段 1 (本地事务): Mongo Tx { Update status → PAID + outbox.Append }
//	阶段 2 (外部副作用): stockGRPC.Confirm(orderID) — held → confirmed
//
// 与 IntakeOrder 的对称性:intake 是 "外部副作用先,本地事务后";confirm 是
// "本地事务先,外部副作用后"。原因:Order 已存在,Confirm 是 idempotent 的
// status 推进,即使后续 Confirm 失败也不会破坏 Order 状态——order 侧 reconcile
// worker (见 internal/order/infra/reconcile/worker.go) 会兜底把已 confirmed Order
// 名下的 held reservation 推到 confirmed。
type ConfirmOrder interface {
	Confirm(ctx context.Context, in ConfirmInput) error
}

type confirmOrder struct {
	orderRepo domain.Repository
	stock     query.StockService
	outbox    domainsvc.OutboxAppender
	tx        domainsvc.TxRunner
}

func NewConfirmOrder(
	orderRepo domain.Repository,
	stock query.StockService,
	outbox domainsvc.OutboxAppender,
	tx domainsvc.TxRunner,
) ConfirmOrder {
	if orderRepo == nil {
		panic("ConfirmOrder: nil orderRepo")
	}
	if stock == nil {
		panic("ConfirmOrder: nil stock")
	}
	if outbox == nil {
		panic("ConfirmOrder: nil outbox")
	}
	if tx == nil {
		panic("ConfirmOrder: nil tx")
	}
	return &confirmOrder{orderRepo: orderRepo, stock: stock, outbox: outbox, tx: tx}
}

// Confirm 推进 Order 到 PAID 并触发 stock.Confirm。
//
// 错误处理:
//   - Mongo tx 失败 → 返回 error,consumer NACK 让 RabbitMQ 重投
//   - Order 已 CANCELLED (StatusConflictError) → 在**同一个 Mongo tx 内**
//     outbox.Append(OrderRefundRequestedEvent),然后返回 conflict error。
//     consumer 收到 conflict 直接 ack;refund 由 outbox.Worker 异步推到
//     payment 服务消费。这条 ADR-0001+ 的硬化把 "决定退款" 和 "发出退款指令"
//     做了原子化,消除了之前 consumer 内联 publish 在 RabbitMQ 临时不可达时
//     丢失退款事件的风险。
//   - stock.Confirm 失败 → 只记日志,order 侧 reconcile worker 兜底(见
//     internal/order/infra/reconcile/worker.go;reservation 从 held → confirmed
//     这一段不是关键路径,可以最终一致)
func (s *confirmOrder) Confirm(ctx context.Context, in ConfirmInput) error {
	if in.OrderID == "" {
		return errors.New("confirm: empty order id")
	}
	if in.CustomerID == "" {
		return errors.New("confirm: empty customer id")
	}

	var conflictForRefund *domain.StatusConflictError

	// 阶段 1: Mongo tx 推 Order 到 PAID,或在 conflict 时同 tx 内写 refund 请求。
	txErr := s.orderRepo.Update(ctx,
		&domain.Order{ID: in.OrderID, CustomerID: in.CustomerID},
		func(sCtx context.Context, oldOrder *domain.Order) (*domain.Order, error) {
			markErr := oldOrder.MarkPaid()
			if markErr == nil {
				// 正常路径:PENDING → PAID。OrderPaidEvent 在 aggregate 上 append 了
				// 但 eventsToOutboxRecords 故意 skip (避免和 payment 已发的 order.paid
				// fanout 形成循环,见 intake.go 翻译表)。所以这里 PullEvents 拿出来也只
				// 是清空,确保不残留到下一次 Update。
				_ = oldOrder.PullEvents()
				return oldOrder, nil
			}

			// 失败路径:检查是否是 conflict + Order 已 CANCELLED + 我们拿到了
			// PaymentIntentID(Stripe Refund 必需)。三者都满足才走 refund 分支。
			var conflict *domain.StatusConflictError
			if errors.As(markErr, &conflict) &&
				oldOrder.Status == orderpb.OrderStatus_ORDER_STATUS_CANCELLED &&
				in.PaymentIntentID != "" {
				if reqErr := oldOrder.RequestRefund(in.PaymentIntentID); reqErr != nil {
					return nil, reqErr
				}
				records, err := eventsToOutboxRecords(oldOrder.PullEvents())
				if err != nil {
					return nil, errors.Wrap(err, "confirm: events translation")
				}
				if len(records) > 0 {
					if appendErr := s.outbox.Append(sCtx, records); appendErr != nil {
						return nil, errors.Wrap(appendErr, "confirm: outbox append refund")
					}
				}
				conflictForRefund = conflict
				return oldOrder, nil
			}

			// 其他 conflict (e.g. 已 PAID — 重复消费 order.paid) 或非 conflict 错误:
			// 让 tx 回滚,bubble 上去。Update 装饰 Wrap 后 caller 凭类型判断。
			return nil, markErr
		},
	)
	if txErr != nil {
		return errors.Wrap(txErr, "confirm: mongo update")
	}

	// Conflict + refund 已 queued 的路径:saga 不再调 stock.Confirm,直接抛 conflict
	// 让 consumer ack。
	if conflictForRefund != nil {
		return conflictForRefund
	}

	// 阶段 2: stock 侧 confirm。失败 best-effort,order 侧 reconcile worker 兜底
	// (见 internal/order/infra/reconcile/worker.go)。
	if err := s.stock.Confirm(ctx, in.OrderID); err != nil {
		return errors.Wrapf(err, "confirm: stock.Confirm (order already PAID; reservation will be reconciled by order-side reconcile worker, see internal/order/infra/reconcile/worker.go)")
	}

	return nil
}

// IsStatusConflict 让 caller 判断 confirm 失败是否因为 Order 已经在
// 非法终态 (e.g. CANCELLED) — 这是要触发 refund 的信号。
func IsStatusConflict(err error) (*domain.StatusConflictError, bool) {
	var conflict *domain.StatusConflictError
	if errors.As(err, &conflict) {
		return conflict, true
	}
	return nil, false
}

// 编译期断言:Order aggregate 必须暴露 MarkPaid。
var _ statusMarker = (*domain.Order)(nil)

type statusMarker interface {
	MarkPaid() error
	UpdateStatus(orderpb.OrderStatus) error
}
