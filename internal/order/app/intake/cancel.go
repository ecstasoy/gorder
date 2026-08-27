package intake

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app/query"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	domainsvc "github.com/ecstasoy/gorder/order/domain/service"
	"github.com/pkg/errors"
)

// CancelInput 是 CancelOrder.Cancel 的输入。来自 payment.timeout 或主动取消。
type CancelInput struct {
	OrderID    string
	CustomerID string
}

// CancelOrder 是 saga 生命周期的第三段(ADR-0002):
//
//	阶段 1 (本地事务): Mongo Tx { o.Cancel() (status + event) + outbox.Append }
//	阶段 2 (外部副作用): stockGRPC.Release(orderID) — held → released
//
// 与 ConfirmOrder 同形。与 IntakeOrder 不同的是:这里 aggregate 已存在,
// Cancel 是 status 推进 + event 记录;Release 是 reservation 反向。
type CancelOrder interface {
	Cancel(ctx context.Context, in CancelInput) (didCancel bool, err error)
}

type cancelOrder struct {
	orderRepo domain.Repository
	stock     query.StockService
	outbox    domainsvc.OutboxAppender
	tx        domainsvc.TxRunner
}

func NewCancelOrder(
	orderRepo domain.Repository,
	stock query.StockService,
	outbox domainsvc.OutboxAppender,
	tx domainsvc.TxRunner,
) CancelOrder {
	if orderRepo == nil {
		panic("CancelOrder: nil orderRepo")
	}
	if stock == nil {
		panic("CancelOrder: nil stock")
	}
	if outbox == nil {
		panic("CancelOrder: nil outbox")
	}
	if tx == nil {
		panic("CancelOrder: nil tx")
	}
	return &cancelOrder{orderRepo: orderRepo, stock: stock, outbox: outbox, tx: tx}
}

// Cancel 把 Order 推到 CANCELLED 并发出 OrderCancelledEvent,然后调 stock.Release。
// 幂等:已 cancelled 时直接返回 didCancel=false,不重复 Release。
//
// PENDING → CANCELLED 是唯一合法转移。其他 status (PAID 等) 走 refund 路径,
// 不应该经过这里。
func (s *cancelOrder) Cancel(ctx context.Context, in CancelInput) (bool, error) {
	if in.OrderID == "" {
		return false, errors.New("cancel: empty order id")
	}

	var didCancel bool

	txErr := s.orderRepo.Update(ctx,
		&domain.Order{ID: in.OrderID, CustomerID: in.CustomerID},
		func(sCtx context.Context, oldOrder *domain.Order) (*domain.Order, error) {
			// 幂等:只 PENDING 时执行 Cancel,否则静默跳过
			if oldOrder.Status != orderpb.OrderStatus_ORDER_STATUS_PENDING {
				return oldOrder, nil
			}
			if err := oldOrder.Cancel(); err != nil {
				return nil, err
			}
			didCancel = true

			// PullEvents + 翻译到 outbox.Record。
			// 注意:Update 的 closure 没有 sessionContext 暴露,所以 outbox 写入
			// 跟 Mongo 状态 update 实际上在同一个 session 里 (因为 Update 内部用
			// session.WithTransaction 包了整个 closure)。
			events := oldOrder.PullEvents()
			records, err := eventsToOutboxRecords(events)
			if err != nil {
				return nil, errors.Wrap(err, "cancel: events translation")
			}
			if len(records) > 0 {
				if appendErr := s.outbox.Append(sCtx, records); appendErr != nil {
					return nil, errors.Wrap(appendErr, "cancel: outbox append")
				}
			}
			return oldOrder, nil
		},
	)
	if txErr != nil {
		return false, errors.Wrap(txErr, "cancel: mongo update")
	}

	// 阶段 2: stock.Release。best-effort —— Release 失败只记日志,order 侧 reconcile worker 兜底
	// (见 internal/order/infra/reconcile/worker.go)。
	if didCancel {
		if relErr := s.stock.Release(ctx, in.OrderID); relErr != nil {
			logging.Errorf(ctx, nil, "cancel: stock.Release failed orderID=%s err=%v (will be reconciled)", in.OrderID, relErr)
		}
	}

	return didCancel, nil
}

// eventsToOutboxRecords 在 intake.go 里定义,所有 saga (intake / confirm / cancel)
// 共用同一翻译表。加新 event 类型时在 intake.go 的 switch 里登记。
