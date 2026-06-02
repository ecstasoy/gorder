package command

import (
	"context"
	"fmt"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app/intake"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

type CreateOrder struct {
	CustomerID string
	Items      []*entity.ItemWithQuantity
}

type CreateOrderResult struct {
	OrderID string
}

type CreateOrderHandler decorator.CommandHandler[CreateOrder, *CreateOrderResult]

type createOrderHandler struct {
	intake intake.IntakeOrder
}

func NewCreateOrderHandler(
	intakeSvc intake.IntakeOrder,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CreateOrderHandler {
	if intakeSvc == nil {
		panic("nil intake")
	}
	return decorator.ApplyCommandDecorators[CreateOrder, *CreateOrderResult](
		createOrderHandler{intake: intakeSvc},
		logger,
		metricsClient,
	)
}

// Handle 退化为参数转换 + saga 调用 —— resolve / reserve / persist + outbox /
// compensate 全在 intake module 内。ADR-0001 Step 5。
func (c createOrderHandler) Handle(ctx context.Context, cmd CreateOrder) (*CreateOrderResult, error) {
	var err error
	defer logging.WhenCommandExecute(ctx, "CreateOrderHandler.Handle", cmd, err)

	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(ctx, fmt.Sprintf("rabbitmq.%s.publish", broker.EventOrderCreated))
	defer span.End()

	o, err := c.intake.Intake(ctx, intake.IntakeInput{
		CustomerID: cmd.CustomerID,
		RawItems:   packItems(cmd.Items),
	})
	if err != nil {
		return nil, err
	}
	return &CreateOrderResult{OrderID: o.ID}, nil
}

// packItems 合并同 ID 行的 quantity —— 客户端传 [{id:A,1},{id:A,2}] 时
// 合并成 [{id:A,3}],避免 reservation 表 UNIQUE KEY 冲突。
func packItems(items []*entity.ItemWithQuantity) []*entity.ItemWithQuantity {
	merged := make(map[string]int32)
	for _, item := range items {
		merged[item.ID] += item.Quantity
	}
	res := make([]*entity.ItemWithQuantity, 0, len(merged))
	for id, quantity := range merged {
		res = append(res, entity.NewItemWithQuantity(id, quantity))
	}
	return res
}
