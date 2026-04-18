package command

import (
	"context"
	"fmt"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app/query"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/pkg/errors"
	amqp "github.com/rabbitmq/amqp091-go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

const flashMetaKeyPrefix = "flash:meta:"

type CreateFlashOrder struct {
	CustomerID string
	Items      []*entity.ItemWithQuantity
}

type CreateFlashOrderResult struct {
	OrderID string
}

type CreateFlashOrderHandler decorator.CommandHandler[CreateFlashOrder, *CreateFlashOrderResult]

type createFlashOrderHandler struct {
	orderRepo   domain.Repository
	stockGRPC   query.StockService
	redisClient *goredis.Client
	channel     *amqp.Channel
}

func NewCreateFlashOrderHandler(
	orderRepo domain.Repository,
	stockGRPC query.StockService,
	redisClient *goredis.Client,
	channel *amqp.Channel,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CreateFlashOrderHandler {
	if orderRepo == nil {
		panic("orderRepo cannot be nil")
	}
	if stockGRPC == nil {
		panic("stockGRPC cannot be nil")
	}
	if redisClient == nil {
		panic("redisClient cannot be nil")
	}
	if channel == nil {
		panic("channel cannot be nil")
	}
	return decorator.ApplyCommandDecorators[CreateFlashOrder, *CreateFlashOrderResult](
		createFlashOrderHandler{
			orderRepo:   orderRepo,
			stockGRPC:   stockGRPC,
			redisClient: redisClient,
			channel:     channel,
		},
		logger,
		metricsClient,
	)
}

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
		return nil, errors.New("order must contain at least one item")
	}

	validItems, err := c.resolveItems(ctx, cmd.Items)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve items")
	}

	protoQty := convertor.NewItemWithQuantityConvertor().EntitiesToProtos(cmd.Items)
	// 方案 B:flash SKU 是独立 product_id,DeductStock 直接 CAS 扣 MySQL,
	// 不再需要 reserve/consume/release 三段式。卖不完的库存留在 flash 行,
	// 由运维手动合并(后续可加 /flash-sale/merge-back 接口)。
	if err = c.stockGRPC.DeductStock(ctx, protoQty); err != nil {
		return nil, errors.Wrap(err, "failed to deduct flash stock")
	}

	o, err := persistAndPublish(ctx, c.orderRepo, c.channel, &domain.Order{
		CustomerID: cmd.CustomerID,
		Items:      validItems,
		Status:     orderpb.OrderStatus_ORDER_STATUS_PENDING,
	})
	if err != nil {
		return nil, err
	}

	return &CreateFlashOrderResult{OrderID: o.ID}, nil
}

func (c createFlashOrderHandler) resolveItems(ctx context.Context, items []*entity.ItemWithQuantity) ([]*entity.Item, error) {
	resolved := make([]*entity.Item, 0, len(items))
	qtyByID := make(map[string]int32, len(items))
	var missingIDs []string

	for _, item := range items {
		qtyByID[item.ID] = item.Quantity

		meta, err := redis.GetFlashMeta(ctx, c.redisClient, flashMetaKeyPrefix+item.ID)
		if err == nil {
			resolved = append(resolved, entity.NewItem(item.ID, meta.Name, item.Quantity, meta.PriceID))
			continue
		}
		if !errors.Is(err, goredis.Nil) {
			logging.Warnf(ctx, nil, "flash meta redis get failed, id=%s err=%v", item.ID, err)
		}
		missingIDs = append(missingIDs, item.ID)
	}

	if len(missingIDs) == 0 {
		return resolved, nil
	}

	protoItems, err := c.stockGRPC.GetItems(ctx, missingIDs)
	if err != nil {
		return nil, errors.Wrapf(err, "resolve items via stock gRPC, ids=%v", missingIDs)
	}
	if len(protoItems) != len(missingIDs) {
		return nil, errors.Errorf("stock gRPC returned %d items for %d ids", len(protoItems), len(missingIDs))
	}

	for _, p := range protoItems {
		resolved = append(resolved, entity.NewItem(p.ID, p.Name, qtyByID[p.ID], p.PriceID))
	}

	return resolved, nil
}
