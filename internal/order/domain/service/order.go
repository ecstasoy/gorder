package service

import (
	"context"
	"encoding/json"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/entity"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// OrderDomainService 是 ADR-0001 Step 5 之前的过渡产物。
// Step 5 会被 internal/order/app/intake/ 下的 saga 取代。
type OrderDomainService struct {
	Repo   domain.Repository
	Outbox OutboxAppender
	Tx     TxRunner
}

func NewOrderDomainService(repo domain.Repository, outbox OutboxAppender, tx TxRunner) *OrderDomainService {
	return &OrderDomainService{Repo: repo, Outbox: outbox, Tx: tx}
}

// CreateOrder 把 Order 写入 + 出站事件都放在一个 Mongo 事务里。
//   - order.created 来自 aggregate 的 PullEvents (ADR-0001 Step 3 落地:
//     aggregate 拥有事件,saga 翻译为 outbox)
//   - payment.delay 不是 domain event —— 它是 saga 为支付超时机制写的
//     基础设施触发器,saga 自己加一条 outbox 记录 (ADR-0001 Step 6 子决定)
//
// 事件落到 outbox,由后台 worker 推到 RabbitMQ。
func (s *OrderDomainService) CreateOrder(ctx context.Context, order *domain.Order) (*entity.Order, error) {
	// 必须收 *Order: OrderCreatedEvent 在 NewPendingOrder 时 capture 了原 Order
	// 的 ref。如果这里收 value,Repo.Create 写的 ID 会写到 value copy,事件持有
	// 的原 ref 看不到 ID,outbox payload 的 ID 字段会是空字符串。
	var created *domain.Order

	err := s.Tx.Run(ctx, func(sCtx context.Context) error {
		c, err := s.Repo.Create(sCtx, order)
		if err != nil {
			return err
		}
		created = c

		records, err := s.eventsToOutboxRecords(c.PullEvents())
		if err != nil {
			return err
		}

		// 支付超时触发器,作为 saga 的基础设施记录追加。
		delayedPayload, err := json.Marshal(c)
		if err != nil {
			return errors.Wrap(err, "marshal payment-delay payload")
		}
		records = append(records, OutboxRecord{
			EventID: uuid.NewString(),
			Dest:    broker.OrderPaymentDelayQueue,
			Kind:    OutboxKindDelayed,
			Payload: delayedPayload,
		})

		return s.Outbox.Append(sCtx, records)
	})
	if err != nil {
		return nil, errors.Wrap(err, "create order tx")
	}

	return &entity.Order{
		ID:          created.ID,
		CustomerID:  created.CustomerID,
		Status:      created.Status,
		PaymentLink: created.PaymentLink,
		Items:       created.Items,
	}, nil
}

// eventsToOutboxRecords 把 aggregate 产出的 domain event 翻译成 outbox 记录。
// 这里的 switch 是 transitional —— Step 5 saga 重新组织时,这层映射会移到
// app/intake/,可能改成 event 自带 routing 元数据或 application 层有注册表。
func (s *OrderDomainService) eventsToOutboxRecords(events []domain.DomainEvent) ([]OutboxRecord, error) {
	records := make([]OutboxRecord, 0, len(events))
	for _, e := range events {
		switch ev := e.(type) {
		case domain.OrderCreatedEvent:
			payload, err := json.Marshal(ev.Order)
			if err != nil {
				return nil, errors.Wrap(err, "marshal OrderCreated payload")
			}
			records = append(records, OutboxRecord{
				EventID: uuid.NewString(),
				Dest:    broker.EventOrderCreated,
				Kind:    OutboxKindQueue,
				Payload: payload,
			})
		default:
			return nil, errors.Errorf("unknown domain event type %q", e.EventType())
		}
	}
	return records, nil
}
