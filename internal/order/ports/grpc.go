package ports

import (
	"context"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/order/app"
	"github.com/ecstasoy/gorder/order/app/command"
	"github.com/ecstasoy/gorder/order/app/query"
	domain "github.com/ecstasoy/gorder/order/domain/order"
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

func (G GRPCServer) UpdateOrder(ctx context.Context, request *orderpb.Order) (_ *emptypb.Empty, err error) {
	logrus.Infof("order_grpc || request_in || request: %v", request)

	order, err := domain.NewOrder(
		request.ID,
		request.CustomerID,
		request.Status.String(),
		request.PaymentLink,
		convertor.NewItemConvertor().ProtosToEntities(request.Items),
	)

	if err != nil {
		err = status.Error(codes.InvalidArgument, err.Error())
		return nil, err
	}

	_, err = G.app.Commands.UpdateOrder.Handle(ctx, command.UpdateOrder{
		Order: order,
		UpdateFunc: func(ctx context.Context, oldOrder *domain.Order) (*domain.Order, error) {
			// 特殊情况: Payment 服务在处理延迟后调 UpdateOrder 写 payment_link,
			// 但订单已被 order.payment.timeout consumer 置为 CANCELLED。
			// 场景约束: 请求的目标 status == PENDING (Payment 只会写 PENDING,
			// 因为它的本意只是补一个 payment_link 到一个尚未支付的订单上)。
			// 这种情况下 payment_link 已经无意义,幂等跳过,让 Payment 侧 ACK 消息。
			//
			// 注意: 这个 skip 只针对 "CANCELLED 订单被请求写 PENDING" 这个窄场景,
			// 不会干扰 order.paid consumer 的退款路径 —— 那条路径走的是 Order 自己的
			// consumer.handleMessage → UpdateOrder.Handle 直接调用, 不经过 gRPC server,
			// 它会让 StatusConflictError 正常传回,从而触发 order.refund 发布。
			if oldOrder.Status == orderpb.OrderStatus_ORDER_STATUS_CANCELLED &&
				request.Status == orderpb.OrderStatus_ORDER_STATUS_PENDING {
				logrus.Warnf("UpdateOrder gRPC: skipping stale payment_link write for cancelled order %s", oldOrder.ID)
				return oldOrder, nil
			}
			if err := oldOrder.UpdateStatus(request.Status); err != nil {
				return nil, err
			}
			if err := oldOrder.UpdatePaymentLink(request.PaymentLink); err != nil {
				return nil, err
			}
			if err := oldOrder.UpdateItems(convertor.NewItemConvertor().ProtosToEntities(request.Items)); err != nil {
				return nil, err
			}
			return oldOrder, nil
		},
	})

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &emptypb.Empty{}, nil
}
