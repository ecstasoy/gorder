package ports

import (
	"context"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/order/app"
	"github.com/ecstasoy/gorder/order/app/command"
	"github.com/ecstasoy/gorder/order/app/query"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type GRPCServer struct {
	app app.Application
}

func NewGRPCServer(app app.Application) *GRPCServer {
	return &GRPCServer{app: app}
}

func (G GRPCServer) CreateOrder(ctx context.Context, request *orderpb.CreateOrderRequest) (*emptypb.Empty, error) {
	_, err := G.app.Commands.CreateOrder.Handle(ctx, command.CreateOrder{
		CustomerID: request.CustomerID,
		Items:      convertor.NewItemWithQuantityConvertor().ProtosToEntities(request.Items),
	})

	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &emptypb.Empty{}, nil
}

func (G GRPCServer) GetOrder(ctx context.Context, request *orderpb.GetOrderRequest) (*orderpb.Order, error) {
	o, err := G.app.Queries.GetCustomerOrder.Handle(ctx, query.GetCustomerOrder{
		CustomerID: request.CustomerID,
		OrderID:    request.OrderID,
	})

	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &orderpb.Order{
		ID:          o.ID,
		CustomerID:  o.CustomerID,
		Status:      o.Status,
		Items:       convertor.NewItemConvertor().EntitiesToProtos(o.Items),
		PaymentLink: o.PaymentLink,
	}, nil
}

// UpdateOrder gRPC 实际只用于 Payment 服务在 Stripe checkout 创建后回写 payment_link。
// ADR-0002 落地后,dispatch 给 typed SetPaymentLink command,
// 状态校验(已 CANCELLED 跳过)在 command 内部完成,这里不再写 closure。
func (G GRPCServer) UpdateOrder(ctx context.Context, request *orderpb.Order) (_ *emptypb.Empty, err error) {
	logrus.Infof("order_grpc || request_in || request: %v", request)

	if request.ID == "" || request.CustomerID == "" {
		return nil, status.Error(codes.InvalidArgument, "order id and customer id required")
	}

	_, err = G.app.Commands.SetPaymentLink.Handle(ctx, command.SetPaymentLink{
		OrderID:     request.ID,
		CustomerID:  request.CustomerID,
		PaymentLink: request.PaymentLink,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &emptypb.Empty{}, nil
}
