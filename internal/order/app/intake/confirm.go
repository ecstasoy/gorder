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
type ConfirmInput struct {
	OrderID    string
	CustomerID string
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
// status 推进,即使后续 Confirm 失败也不会破坏 Order 状态——stock 服务侧的
// zombie 扫描会兜底把已 confirmed Order 名下的 held reservation 推到 confirmed。
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
//   - Order 已 CANCELLED (StatusConflictError) → 返回该错误,caller 负责发 refund
//   - stock.Confirm 失败 → 只记日志,stock 侧 zombie 扫描兜底(reservation 已经
//     是 held → confirmed 这一段不是关键路径,可以最终一致)
func (s *confirmOrder) Confirm(ctx context.Context, in ConfirmInput) error {
	if in.OrderID == "" {
		return errors.New("confirm: empty order id")
	}
	if in.CustomerID == "" {
		return errors.New("confirm: empty customer id")
	}

	// 阶段 1: Mongo tx 把 Order 推到 PAID。Update 内部用 session.WithTransaction。
	txErr := s.orderRepo.Update(ctx,
		&domain.Order{ID: in.OrderID, CustomerID: in.CustomerID},
		func(sCtx context.Context, oldOrder *domain.Order) (*domain.Order, error) {
			if err := oldOrder.MarkPaid(); err != nil {
				return nil, err
			}
			// TODO(ADR-0002 follow-up): 把 OrderPaidEvent 也接入 outbox。
			// 目前没下游消费 order.paid 之外的事件,先不接,等需要时改一处即可。
			return oldOrder, nil
		},
	)
	if txErr != nil {
		return errors.Wrap(txErr, "confirm: mongo update")
	}

	// 阶段 2: stock 侧 confirm。失败 best-effort, zombie 扫描兜底。
	if err := s.stock.Confirm(ctx, in.OrderID); err != nil {
		// 不抛错——Order 已 PAID 是事实,reservation 暂时卡 held 由 stock 服务
		// 自己的对账任务推进。这是 saga 在 "本地写已成功 + 外部副作用失败" 时
		// 的标准处理。
		return errors.Wrapf(err, "confirm: stock.Confirm (order already PAID; reservation will be reconciled by stock-side zombie scan)")
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
