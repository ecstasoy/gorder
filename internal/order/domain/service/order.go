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

// CreateOrder 把 Order 写入 + 两条出站事件 (order.created + payment.delayed)
// 都放在一个 Mongo 事务里。事件落到 outbox,由后台 worker 推到 RabbitMQ。
func (s *OrderDomainService) CreateOrder(ctx context.Context, order domain.Order) (*entity.Order, error) {
	var created *domain.Order

	err := s.Tx.Run(ctx, func(sCtx context.Context) error {
		c, err := s.Repo.Create(sCtx, &order)
		if err != nil {
			return err
		}
		created = c

		payload, err := json.Marshal(c)
		if err != nil {
			return errors.Wrap(err, "marshal order payload")
		}

		records := []OutboxRecord{
			{
				EventID: uuid.NewString(),
				Dest:    broker.EventOrderCreated,
				Kind:    OutboxKindQueue,
				Payload: payload,
			},
			{
				EventID: uuid.NewString(),
				Dest:    broker.OrderPaymentDelayQueue, // 记录用,worker dispatch 时按 Kind 走 PublishDelayed
				Kind:    OutboxKindDelayed,
				Payload: payload,
			},
		}
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
