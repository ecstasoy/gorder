package command

import (
	"context"
	"fmt"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app/intake"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

type CreateFlashOrder struct {
	CustomerID string
	Items      []*entity.ItemWithQuantity
}

type CreateFlashOrderResult struct {
	OrderID string
}

type CreateFlashOrderHandler decorator.CommandHandler[CreateFlashOrder, *CreateFlashOrderResult]

type createFlashOrderHandler struct {
	flashIntake intake.IntakeOrder
}

func NewCreateFlashOrderHandler(
	flashIntake intake.IntakeOrder,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CreateFlashOrderHandler {
	if flashIntake == nil {
		panic("nil flashIntake")
	}
	return decorator.ApplyCommandDecorators[CreateFlashOrder, *CreateFlashOrderResult](
		createFlashOrderHandler{flashIntake: flashIntake},
		logger,
		metricsClient,
	)
}

// Handle 退化为参数转换 + saga 调用。flash 与常规 intake 共用同一个
// IntakeOrder seam,区别只在构造时注入的 ItemResolver (Redis 优先 vs
// 仅 Catalog) —— ADR-0001 "two adapters justify the seam"。
//
// 旧版本里手写的 DeductStock + on-error RestoreStock 补偿块已经被
// saga 内部的 Reserve + Release 替代,不再重复。
func (c createFlashOrderHandler) Handle(ctx context.Context, cmd CreateFlashOrder) (*CreateFlashOrderResult, error) {
	var err error
	defer logging.WhenCommandExecute(ctx, "CreateFlashOrderHandler.Handle", CreateFlashOrder{
		CustomerID: cmd.CustomerID,
		Items:      cmd.Items,
	}, err)

	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(ctx, fmt.Sprintf("rabbitmq.%s.publish", broker.EventOrderCreated))
	defer span.End()

	if len(cmd.Items) == 0 {
		return nil, errors.New("flash order must contain at least one item")
	}

	o, err := c.flashIntake.Intake(ctx, intake.IntakeInput{
		CustomerID: cmd.CustomerID,
		RawItems:   packItems(cmd.Items),
	})
	if err != nil {
		return nil, err
	}
	return &CreateFlashOrderResult{OrderID: o.ID}, nil
}
