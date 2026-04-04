package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/ecstasoy/gorder/order/app/query"
	"github.com/sirupsen/logrus"
)

type CancelOrder struct {
	OrderID    string
	CustomerID string
}

type CancelOrderResult struct{}

type CancelOrderHandler decorator.CommandHandler[CancelOrder, *CancelOrderResult]

type cancelOrderHandler struct {
	orderRepo domain.Repository
	stockGRPC query.StockService
}

func NewCancelOrderHandler(
	orderRepo domain.Repository,
	stockGRPC query.StockService,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CancelOrderHandler {
	if orderRepo == nil {
		panic("orderRepo cannot be nil")
	}
	return decorator.ApplyCommandDecorators[CancelOrder, *CancelOrderResult](
		cancelOrderHandler{
			orderRepo: orderRepo,
			stockGRPC: stockGRPC,
		},
		logger,
		metricsClient,
	)
}

func (h cancelOrderHandler) Handle(ctx context.Context, cmd CancelOrder) (*CancelOrderResult, error) {
	var cancelledItems []*entity.Item

	err := h.orderRepo.Update(ctx,
		&domain.Order{ID: cmd.OrderID, CustomerID: cmd.CustomerID},
		func(ctx context.Context, o *domain.Order) (*domain.Order, error) {
			if o.Status != orderpb.OrderStatus_ORDER_STATUS_PENDING {
				return o, nil
			}
			if err := o.UpdateStatus(orderpb.OrderStatus_ORDER_STATUS_CANCELLED); err != nil {
				return nil, err
			}
			cancelledItems = o.Items
			return o, nil
		},
	)
	if err != nil {
		return nil, err
	}

	// 只有实际取消了（cancelledItems 不为空）才归还库存
	if len(cancelledItems) > 0 {
		itemsWithQty := make([]*orderpb.ItemWithQuantity, 0, len(cancelledItems))
		for _, item := range convertor.NewItemConvertor().EntitiesToProtos(cancelledItems) {
			itemsWithQty = append(itemsWithQty, &orderpb.ItemWithQuantity{
				ItemID:   item.ID,
				Quantity: item.Quantity,
			})
		}
		if err := h.stockGRPC.RestoreStock(ctx, itemsWithQty); err != nil {
			// 订单已取消成功，库存归还失败只记录日志，不回滚
			logrus.WithContext(ctx).Errorf("order %s cancelled but failed to restore stock: %v", cmd.OrderID, err)
		}
	}

	return &CancelOrderResult{}, nil
}
