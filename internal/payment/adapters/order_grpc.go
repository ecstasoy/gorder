package adapters

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/tracing"
	"google.golang.org/grpc/status"
)

type OrderGRPC struct {
	client orderpb.OrderServiceClient
}

func NewOrderGRPC(client orderpb.OrderServiceClient) *OrderGRPC {
	return &OrderGRPC{client: client}
}

func (o OrderGRPC) UpdateOrder(ctx context.Context, order *orderpb.Order) (err error) {
	ctx, span := tracing.Start(ctx, "order_grpc.update_order")
	defer span.End()

	_, err = o.client.UpdateOrder(ctx, order)
	return status.Convert(err).Err()
}

func (o OrderGRPC) GetOrder(ctx context.Context, orderID, customerID string) (*orderpb.Order, error) {
	ctx, span := tracing.Start(ctx, "order_grpc.get_order")
	defer span.End()

	order, err := o.client.GetOrder(ctx, &orderpb.GetOrderRequest{
		OrderID:    orderID,
		CustomerID: customerID,
	})
	return order, status.Convert(err).Err()
}
