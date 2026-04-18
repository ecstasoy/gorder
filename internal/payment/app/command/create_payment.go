package command

import (
	"context"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/payment/domain"
	"github.com/sirupsen/logrus"
)

type CreatePayment struct {
	Order *entity.Order
}

type CreatePaymentHandler decorator.CommandHandler[CreatePayment, string]

type createPaymentHandler struct {
	processor domain.Processor
	orderGRPC OrderService
}

func NewCreatePaymentHandler(
	processor domain.Processor,
	orderGRPC OrderService,
	logger *logrus.Logger,
	metricClient decorator.MetricsClient,
) CreatePaymentHandler {
	return decorator.ApplyCommandDecorators[CreatePayment, string](
		createPaymentHandler{processor: processor, orderGRPC: orderGRPC},
		logger,
		metricClient,
	)
}

func (c createPaymentHandler) Handle(ctx context.Context, cmd CreatePayment) (link string, err error) {
	// 注意: defer 里引用命名返回值 err (通过闭包捕获变量地址),
	// 这样函数返回时的真实 err 才会被日志看到。
	// 不能写 defer logging.WhenCommandExecute(ctx, ..., cmd, err),那样 err 在 defer
	// 注册时按值捕获 (nil 零值),日志永远显示 success — 这是之前的一个隐形 bug。
	defer func() {
		logging.WhenCommandExecute(ctx, "CreatePaymentHandler.Handle", cmd, err)
	}()

	link, err = c.processor.CreatePaymentLink(ctx, cmd.Order)
	if err != nil {
		return "", err
	}
	newOrder, err := entity.NewValidOrder(
		cmd.Order.ID,
		cmd.Order.CustomerID,
		orderpb.OrderStatus_ORDER_STATUS_PENDING,
		link,
		cmd.Order.Items,
	)
	if err != nil {
		return "", err
	}
	err = c.orderGRPC.UpdateOrder(ctx, convertor.NewOrderConvertor().EntityToProto(newOrder))
	return link, err
}
