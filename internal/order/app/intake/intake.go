package intake

import (
	"context"
	"encoding/json"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app/query"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	domainsvc "github.com/ecstasoy/gorder/order/domain/service"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// IntakeInput 是 IntakeOrder.Intake 的输入 —— customer + 未解析的 (id, qty)
// 列表。变体差异 (常规 vs 闪购) 通过构造 IntakeOrder 时注入不同的 ItemResolver
// 体现,不在 input 里。
type IntakeInput struct {
	CustomerID string
	RawItems   []*entity.ItemWithQuantity
}

// IntakeOrder 是订单收单的 deep module。一个 method、interface 极窄,
// implementation 持有完整生命周期:resolve → reserve → persist + outbox →
// 失败时 release 补偿。Step 5 落地后,handler 退化为参数转换 + 调它。
type IntakeOrder interface {
	Intake(ctx context.Context, in IntakeInput) (*entity.Order, error)
}

type intakeOrder struct {
	resolver  ItemResolver
	stock     query.StockService
	orderRepo domain.Repository
	outbox    domainsvc.OutboxAppender
	tx        domainsvc.TxRunner
}

func NewIntakeOrder(
	resolver ItemResolver,
	stock query.StockService,
	orderRepo domain.Repository,
	outbox domainsvc.OutboxAppender,
	tx domainsvc.TxRunner,
) IntakeOrder {
	if resolver == nil {
		panic("IntakeOrder: nil resolver")
	}
	if stock == nil {
		panic("IntakeOrder: nil stock")
	}
	if orderRepo == nil {
		panic("IntakeOrder: nil orderRepo")
	}
	if outbox == nil {
		panic("IntakeOrder: nil outbox")
	}
	if tx == nil {
		panic("IntakeOrder: nil tx")
	}
	return &intakeOrder{resolver: resolver, stock: stock, orderRepo: orderRepo, outbox: outbox, tx: tx}
}

// Intake 是 saga 入口,按 ADR-0001 的三段式跑:
//
//	阶段 1 (外部副作用): resolver.Resolve + stock.Reserve(orderID, items)。
//	  失败 → 直接返回 (Reserve 自身是事务性的,不会有半锁状态)。
//	阶段 2 (本地事务): Mongo tx 里 repo.Create + outbox.Append。
//	  失败 → 调 stock.Release(orderID) 把 Reserve 回滚 (补偿)。
//	阶段 3 (事件分发): 由 outbox worker 异步推 RabbitMQ,不在本方法里。
//
// OrderID 在 Reserve 前生成 —— 用 bson.NewObjectID().Hex() 作为幂等键,
// 同一个 ID 落到 Mongo 的 _id (见 OrderRepositoryMongo.Create 改造)。
func (s *intakeOrder) Intake(ctx context.Context, in IntakeInput) (*entity.Order, error) {
	if len(in.RawItems) == 0 {
		return nil, errors.New("intake: no items provided")
	}
	if in.CustomerID == "" {
		return nil, errors.New("intake: empty customer id")
	}

	// 1. Resolve 商品元数据。
	items, err := s.resolver.Resolve(ctx, in.RawItems)
	if err != nil {
		return nil, errors.Wrap(err, "intake: resolve")
	}

	// 2. Build aggregate (此时 NewPendingOrder 已 append OrderCreatedEvent)。
	pending, err := domain.NewPendingOrder(in.CustomerID, items)
	if err != nil {
		return nil, errors.Wrap(err, "intake: new pending order")
	}

	// 3. 早绑定 OrderID —— Reserve 需要它作为幂等键,Repo.Create 会用同一个 ID 落 _id。
	pending.ID = bson.NewObjectID().Hex()

	// 4. 阶段 1: Reserve。失败直接返回,没有需要补偿的状态。
	reserveItems := convertor.NewItemWithQuantityConvertor().EntitiesToProtos(toIWQ(items))
	if err := s.stock.Reserve(ctx, pending.ID, reserveItems); err != nil {
		return nil, errors.Wrap(err, "intake: stock reserve")
	}

	// 5. 阶段 2: 本地事务里 persist + outbox。失败 → 阶段 1 补偿。
	var created *domain.Order
	txErr := s.tx.Run(ctx, func(sCtx context.Context) error {
		c, err := s.orderRepo.Create(sCtx, pending)
		if err != nil {
			return err
		}
		created = c

		records, err := eventsToOutboxRecords(c.PullEvents())
		if err != nil {
			return err
		}

		// payment.delay 不是 domain event —— saga 加一条 outbox 触发器。
		delayedPayload, err := json.Marshal(c)
		if err != nil {
			return errors.Wrap(err, "marshal payment-delay payload")
		}
		records = append(records, domainsvc.OutboxRecord{
			EventID: uuid.NewString(),
			Dest:    broker.OrderPaymentDelayQueue,
			Kind:    domainsvc.OutboxKindDelayed,
			Payload: delayedPayload,
		})

		return s.outbox.Append(sCtx, records)
	})
	if txErr != nil {
		// 阶段 1 补偿:释放 Reserve。补偿失败只记日志 —— stock 侧有 zombie 扫描兜底。
		if relErr := s.stock.Release(context.Background(), pending.ID); relErr != nil {
			logging.Errorf(ctx, nil, "intake: stock release compensation failed orderID=%s err=%v", pending.ID, relErr)
		}
		return nil, errors.Wrap(txErr, "intake: persist tx")
	}

	return &entity.Order{
		ID:          created.ID,
		CustomerID:  created.CustomerID,
		Status:      created.Status,
		PaymentLink: created.PaymentLink,
		Items:       created.Items,
	}, nil
}

// eventsToOutboxRecords 把 aggregate 产出的 domain event 翻译为 outbox 记录。
// 所有 saga (intake / confirm / cancel) 共用同一翻译表 (ADR-0002)。
// 加新 event 类型时必须在这里登记,否则 saga 在 PullEvents 后会 return error
// —— 这是有意的,aggregate 不该悄悄长出 saga 不认识的 event。
func eventsToOutboxRecords(events []domain.DomainEvent) ([]domainsvc.OutboxRecord, error) {
	records := make([]domainsvc.OutboxRecord, 0, len(events))
	for _, e := range events {
		switch ev := e.(type) {
		case domain.OrderCreatedEvent:
			payload, err := json.Marshal(ev.Order)
			if err != nil {
				return nil, errors.Wrap(err, "marshal OrderCreated")
			}
			records = append(records, domainsvc.OutboxRecord{
				EventID: uuid.NewString(),
				Dest:    broker.EventOrderCreated,
				Kind:    domainsvc.OutboxKindQueue,
				Payload: payload,
			})
		case domain.OrderPaidEvent:
			// 注意:不要把 OrderPaidEvent bridge 到 outbox。order.paid 由 payment
			// 服务在 Stripe webhook 时直接发出 (fanout exchange,kitchen + order
			// 两侧消费)。如果 order 的 ConfirmOrder saga 也把它发到 outbox,会和
			// payment 的发布产生循环:order.paid → confirm saga → 再 publish
			// order.paid → 再 confirm... 永远停不下来。
			// OrderPaidEvent 留在 aggregate 上作为 "状态转移的领域记录",但不出 saga。
			_ = ev
			continue
		case domain.OrderCancelledEvent:
			payload, err := json.Marshal(ev.Order)
			if err != nil {
				return nil, errors.Wrap(err, "marshal OrderCancelled")
			}
			records = append(records, domainsvc.OutboxRecord{
				EventID: uuid.NewString(),
				Dest:    broker.EventOrderCancelled, // direct queue;未来下游 (refund / notification) 订阅
				Kind:    domainsvc.OutboxKindQueue,
				Payload: payload,
			})
		case domain.OrderRefundRequestedEvent:
			// ConfirmOrder saga 在 StatusConflictError + Order=CANCELLED 时
			// append。Payload 与 broker.OrderRefundPayload 字段对齐,payment
			// 消费 order.refund queue 时直接 unmarshal。
			refundEventID := uuid.NewString()
			payload, err := json.Marshal(broker.OrderRefundPayload{
				OrderID:         ev.Order.ID,
				CustomerID:      ev.Order.CustomerID,
				PaymentIntentID: ev.PaymentIntentID,
				EventID:         refundEventID, // 用作 Stripe Idempotency-Key
			})
			if err != nil {
				return nil, errors.Wrap(err, "marshal OrderRefundRequested")
			}
			records = append(records, domainsvc.OutboxRecord{
				EventID: refundEventID,
				Dest:    broker.EventOrderRefund,
				Kind:    domainsvc.OutboxKindQueue,
				Payload: payload,
			})
		case domain.OrderRefundedEvent:
			// payment 服务转发 Stripe charge.refunded webhook → order.refunded queue
			// → order 消费后 MarkRefunded → 这条事件 append。目前无下游订阅
			// order.refunded.completed 类的事件;先不接 outbox,等下游需要时
			// 加一行 case 即可。
			_ = ev
			continue
		default:
			return nil, errors.Errorf("intake: unknown event type %q", e.EventType())
		}
	}
	return records, nil
}

// toIWQ 把 []*Item 转成 []*ItemWithQuantity 给 Reserve 调用 —— Reserve 只需 ID + Quantity。
func toIWQ(items []*entity.Item) []*entity.ItemWithQuantity {
	out := make([]*entity.ItemWithQuantity, 0, len(items))
	for _, it := range items {
		out = append(out, &entity.ItemWithQuantity{ID: it.ID, Quantity: it.Quantity})
	}
	return out
}
