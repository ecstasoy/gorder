package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/logging"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/sirupsen/logrus"
)

type UpdateOrder struct {
	Order      *domain.Order
	UpdateFunc func(context.Context, *domain.Order) (*domain.Order, error)
}

type UpdateOrderHandler decorator.CommandHandler[UpdateOrder, interface{}]

type updateOrderHandler struct {
	orderRepo domain.Repository
}

func NewUpdateOrderHandler(
	orderRepo domain.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) UpdateOrderHandler {
	if orderRepo == nil {
		panic("orderRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[UpdateOrder, interface{}](
		updateOrderHandler{orderRepo: orderRepo},
		logger,
		metricsClient,
	)
}

func (c updateOrderHandler) Handle(ctx context.Context, cmd UpdateOrder) (interface{}, error) {
	var err error
	defer logging.WhenCommandExecute(ctx, "UpdateOrderHandler.Handle", cmd.Order, err)
	if cmd.UpdateFunc == nil {
		logrus.Warnf("updateOrderHandler got nil UpdateFn, order=%#v", cmd.Order)
		cmd.UpdateFunc = func(_ context.Context, order *domain.Order) (*domain.Order, error) { return order, nil }
	}
	if err := c.orderRepo.Update(ctx, cmd.Order, cmd.UpdateFunc); err != nil {
		return nil, err
	}
	return nil, nil
}
