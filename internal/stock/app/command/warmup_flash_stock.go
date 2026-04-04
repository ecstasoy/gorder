package command

import (
	"context"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/handler/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const FlashStockKeyPrefix = "flash:stock:"

type WarmUpFlashStock struct {
	Items      []*entity.ItemWithQuantity
	TTLSeconds int64
}

type WarmUpFlashStockHandler decorator.CommandHandler[WarmUpFlashStock, struct{}]

type warmUpFlashStockHandler struct {
	redisClient *goredis.Client
}

func NewWarmUpFlashStockHandler(
	redisClient *goredis.Client,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) WarmUpFlashStockHandler {
	if redisClient == nil {
		panic("redisClient cannot be nil")
	}
	return decorator.ApplyCommandDecorators[WarmUpFlashStock, struct{}](
		warmUpFlashStockHandler{redisClient: redisClient},
		logger,
		metricsClient,
	)
}

func (h warmUpFlashStockHandler) Handle(ctx context.Context, cmd WarmUpFlashStock) (struct{}, error) {
	ttl := time.Duration(cmd.TTLSeconds) * time.Second
	for _, item := range cmd.Items {
		key := fmt.Sprintf("%s%s", FlashStockKeyPrefix, item.ID)
		if err := redis.SetFlashStock(ctx, h.redisClient, key, item.Quantity, ttl); err != nil {
			return struct{}{}, fmt.Errorf("failed to set flash stock for item %s: %w", item.ID, err)
		}
	}

	return struct{}{}, nil
}
