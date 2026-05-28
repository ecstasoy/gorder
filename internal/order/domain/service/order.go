package service

import (
	"context"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/entity"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/pkg/errors"
)

type OrderDomainService struct {
	Repo      domain.Repository
	Publisher broker.Publisher
}

func NewOrderDomainService(repo domain.Repository, publisher broker.Publisher) *OrderDomainService {
	return &OrderDomainService{Repo: repo, Publisher: publisher}
}

func (s *OrderDomainService) CreateOrder(ctx context.Context, order domain.Order) (*entity.Order, error) {
	o, err := s.Repo.Create(ctx, &order)
	if err != nil {
		return nil, err
	}

	if err = s.Publisher.Publish(ctx, broker.DomainEvent{
		Dest: broker.EventOrderCreated,
		Data: o,
	}); err != nil {
		return nil, errors.Wrapf(err, "publish %s", broker.EventOrderCreated)
	}

	// 写入支付超时延迟队列，到期由 DLX 路由到 order.payment.timeout，
	// 触发 CancelOrder。TTL 由队列声明中的 x-message-ttl 决定。
	if err = s.Publisher.PublishDelayed(ctx, broker.DomainEvent{Data: o}); err != nil {
		return nil, errors.Wrapf(err, "publish delayed %s", broker.OrderPaymentDelayQueue)
	}

	return &entity.Order{
		ID:          o.ID,
		CustomerID:  o.CustomerID,
		Status:      o.Status,
		PaymentLink: o.PaymentLink,
		Items:       o.Items,
	}, nil
}
