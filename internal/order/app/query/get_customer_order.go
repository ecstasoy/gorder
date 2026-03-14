package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/sirupsen/logrus"
)

type GetCustomerOrder struct {
	CustomerID string
	OrderID    string
}

type GetCustomerOrderHandler decorator.QueryHandler[GetCustomerOrder, *domain.Order]

type getCusromerOrderHandler struct {
	orderRepo domain.Repository
}

func (g getCusromerOrderHandler) Handle(ctx context.Context, query GetCustomerOrder) (*domain.Order, error) {
	o, err := g.orderRepo.Get(ctx, query.OrderID, query.CustomerID)
	if err != nil {
		return nil, err
	}

	return o, nil
}

func NewGetCustomerOrderHandler(
	orderRepo domain.Repository,
	logger *logrus.Entry,
	metricsClient decorator.MetricsClient,
) GetCustomerOrderHandler {
	if orderRepo == nil {
		panic("orderRepo cannot be nil")
	}

	return decorator.ApplyQueryDecorators[GetCustomerOrder, *domain.Order](
		getCusromerOrderHandler{orderRepo: orderRepo},
		logger,
		metricsClient,
	)
}
