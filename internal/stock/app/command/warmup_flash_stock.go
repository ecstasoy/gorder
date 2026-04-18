package command

import (
	"context"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/handler/redis"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/ecstasoy/gorder/stock/infra/integration"
	goredis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const FlashStockKeyPrefix = "flash:stock:"
const FlashMetaKeyPrefix = "flash:meta:"

type WarmUpFlashStock struct {
	Items      []*entity.ItemWithQuantity
	TTLSeconds int64
}

type WarmUpFlashStockHandler decorator.CommandHandler[WarmUpFlashStock, struct{}]

type warmUpFlashStockHandler struct {
	stockRepo   domain.Repository
	stripeAPI   *integration.StripeAPI
	redisClient *goredis.Client
}

func NewWarmUpFlashStockHandler(
	stockRepo domain.Repository,
	stripeAPI *integration.StripeAPI,
	redisClient *goredis.Client,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) WarmUpFlashStockHandler {
	if stockRepo == nil {
		panic("stockRepo cannot be nil")
	}
	if stripeAPI == nil {
		panic("stripeAPI cannot be nil")
	}
	if redisClient == nil {
		panic("redisClient cannot be nil")
	}
	return decorator.ApplyCommandDecorators[WarmUpFlashStock, struct{}](
		warmUpFlashStockHandler{
			stockRepo:   stockRepo,
			stripeAPI:   stripeAPI,
			redisClient: redisClient,
		},
		logger,
		metricsClient,
	)
}

func (h warmUpFlashStockHandler) Handle(ctx context.Context, cmd WarmUpFlashStock) (struct{}, error) {
	ttl := time.Duration(cmd.TTLSeconds) * time.Second

	// Step 1: SET MySQL flash SKU 行的 quantity 为本场活动总量(方案 B —— 独立 product_id,
	// flash 行和 regular 行物理分离,不再有 reserved_for_flash 列)。
	if err := h.stockRepo.UpsertStock(ctx, cmd.Items); err != nil {
		return struct{}{}, fmt.Errorf("upsert flash stock in mysql: %w", err)
	}

	// Step 2: 缓存 Stripe 元数据到 Redis,秒杀消费者读 flash:meta 避免每单调 Stripe
	for _, item := range cmd.Items {
		product, err := h.stripeAPI.GetProductByID(ctx, item.ID)
		if err != nil {
			return struct{}{}, fmt.Errorf("fetch product info from stripe for item %s: %w", item.ID, err)
		}
		metaKey := fmt.Sprintf("%s%s", FlashMetaKeyPrefix, item.ID)
		metaPayload := redis.FlashMeta{
			Name:    product.Name,
			PriceID: product.DefaultPrice.ID,
		}
		if err := redis.SetFlashMeta(ctx, h.redisClient, metaKey, metaPayload, ttl); err != nil {
			return struct{}{}, fmt.Errorf("failed to set flash meta for item %s: %w", item.ID, err)
		}
	}

	// Step 3: Redis flash:stock 计数器,Lua 原子 reserve 用
	for _, item := range cmd.Items {
		key := fmt.Sprintf("%s%s", FlashStockKeyPrefix, item.ID)
		if err := redis.SetFlashStock(ctx, h.redisClient, key, item.Quantity, ttl); err != nil {
			return struct{}{}, fmt.Errorf("failed to set flash stock for item %s: %w", item.ID, err)
		}
	}

	return struct{}{}, nil
}
