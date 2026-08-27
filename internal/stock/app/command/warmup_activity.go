package command

import (
	"context"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/stock/domain/activity"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/ecstasoy/gorder/stock/infra/integration"
	"github.com/pkg/errors"
	goredis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// WarmUpActivity 按 activity_id 把 Redis 状态推到 active。幂等 —— 同 ID 多次
// 调用都返回成功,真正的写入只发生一次(activity.WarmupDone 标志位 + Lua
// EXISTS 检查)。
//
// 流程:
//   1. 查 activity:不存在 → NotFound;已 ended/cancelled → 拒绝
//   2. 已 WarmupDone → 直接返回 skipped=true
//   3. UpsertStock(product_id, total_stock) 写 MySQL
//   4. 缓存 Stripe meta 到 Redis flash:meta:activity_{id}:{product_id}
//   5. SetFlashStock 到 Redis flash:stock:activity_{id}:{product_id} (带 hash tag)
//   6. activity.MarkWarmedUp + repo.Update
type WarmUpActivity struct {
	ActivityID string
}

type WarmUpActivityResult struct {
	Skipped bool // true = 已经 warmup 过,本次 no-op
}

type WarmUpActivityHandler decorator.CommandHandler[WarmUpActivity, *WarmUpActivityResult]

type warmUpActivityHandler struct {
	activityRepo activity.Repository
	stockRepo    domain.Repository
	stripeAPI    *integration.StripeAPI
	redisClient  *goredis.Client
}

func NewWarmUpActivityHandler(
	activityRepo activity.Repository,
	stockRepo domain.Repository,
	stripeAPI *integration.StripeAPI,
	redisClient *goredis.Client,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) WarmUpActivityHandler {
	if activityRepo == nil {
		panic("nil activityRepo")
	}
	if stockRepo == nil {
		panic("nil stockRepo")
	}
	if stripeAPI == nil {
		panic("nil stripeAPI")
	}
	if redisClient == nil {
		panic("nil redisClient")
	}
	return decorator.ApplyCommandDecorators(
		warmUpActivityHandler{
			activityRepo: activityRepo,
			stockRepo:    stockRepo,
			stripeAPI:    stripeAPI,
			redisClient:  redisClient,
		},
		logger,
		metricsClient,
	)
}

func (h warmUpActivityHandler) Handle(ctx context.Context, cmd WarmUpActivity) (*WarmUpActivityResult, error) {
	a, err := h.activityRepo.Get(ctx, cmd.ActivityID)
	if err != nil {
		return nil, errors.Wrap(err, "WarmUpActivity: get")
	}

	if a.WarmupDone {
		return &WarmUpActivityResult{Skipped: true}, nil
	}
	if a.Status == activity.StatusEnded || a.Status == activity.StatusCancelled {
		return nil, &activity.StatusError{Current: a.Status, Wanted: "warmup"}
	}

	// TTL = endTime - now (允许 warmup 提前于 startTime,key 在活动期内一直有效)
	ttl := time.Until(a.EndTime)
	if ttl <= 0 {
		return nil, errors.New("WarmUpActivity: end_time already passed")
	}

	// Step 1: MySQL 库存
	items := []*entity.ItemWithQuantity{{ID: a.ProductID, Quantity: a.TotalStock}}
	if err := h.stockRepo.UpsertStock(ctx, items); err != nil {
		return nil, errors.Wrap(err, "WarmUpActivity: UpsertStock")
	}

	// Step 2 + 3: Redis meta + stock,新 key 格式带 activity_id + hash tag 于 product_id
	product, err := h.stripeAPI.GetProductByID(ctx, a.ProductID)
	if err != nil {
		return nil, errors.Wrapf(err, "WarmUpActivity: stripe for %s", a.ProductID)
	}
	metaKey := ActivityFlashMetaKey(a.ID, a.ProductID)
	if err := redis.SetFlashMeta(ctx, h.redisClient, metaKey, redis.FlashMeta{
		Name:    product.Name,
		PriceID: product.DefaultPrice.ID,
	}, ttl); err != nil {
		return nil, errors.Wrap(err, "WarmUpActivity: SetFlashMeta")
	}

	stockKey := ActivityFlashStockKey(a.ID, a.ProductID)
	if err := redis.SetFlashStock(ctx, h.redisClient, stockKey, a.TotalStock, ttl); err != nil {
		return nil, errors.Wrap(err, "WarmUpActivity: SetFlashStock")
	}

	// Step 4: 推进状态
	if err := a.MarkWarmedUp(); err != nil {
		return nil, errors.Wrap(err, "WarmUpActivity: MarkWarmedUp")
	}
	if err := h.activityRepo.Update(ctx, a); err != nil {
		return nil, errors.Wrap(err, "WarmUpActivity: persist")
	}

	return &WarmUpActivityResult{Skipped: false}, nil
}

// ActivityFlashStockKey 构造 flash sale stock key,带 activity 前缀 + hash tag。
// 格式:`flash:stock:activity_{id}:{product_id}` —— hash tag 锁定到 product_id,
// 同一 SKU 的 stock / once / meta 必然落 cluster 同 slot;不同 SKU 自然分散。
func ActivityFlashStockKey(activityID, productID string) string {
	return fmt.Sprintf("flash:stock:activity_%s:{%s}", activityID, productID)
}

// ActivityFlashMetaKey 同上,meta key。
func ActivityFlashMetaKey(activityID, productID string) string {
	return fmt.Sprintf("flash:meta:activity_%s:{%s}", activityID, productID)
}

// ActivityFlashOnceKey 同上,一人一单 key。caller (order 侧 HTTP / consumer) 用。
func ActivityFlashOnceKey(activityID, customerID, productID string) string {
	return fmt.Sprintf("flash:once:activity_%s:%s:{%s}", activityID, customerID, productID)
}
