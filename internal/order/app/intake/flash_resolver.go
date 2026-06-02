package intake

import (
	"context"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app/query"
	"github.com/pkg/errors"
	goredis "github.com/redis/go-redis/v9"
)

// flashMetaKeyPrefix 是 warmup 时写入 Redis 的 key 前缀,与
// /flash-sale/warmup 路径写入时使用的一致 (见 order/http.go).
const flashMetaKeyPrefix = "flash:meta:"

// FlashResolver 是 ItemResolver 的闪购变体 —— Redis 热数据优先,缺失项回退
// 到 stock 服务的 GetItems。两个数据源都拿不到的 ID 是硬失败,与
// CatalogResolver 的 "全或无" 风格保持一致。
//
// fallback 链完全在本 adapter 内部:saga 看到的还是 Resolve(raw) → items,
// 不知道有 Redis、不知道有 fallback (ADR-0001 Q7)。
type FlashResolver struct {
	redisClient *goredis.Client
	stockGRPC   query.StockService
}

func NewFlashResolver(redisClient *goredis.Client, stockGRPC query.StockService) *FlashResolver {
	if redisClient == nil {
		panic("FlashResolver: redisClient cannot be nil")
	}
	if stockGRPC == nil {
		panic("FlashResolver: stockGRPC cannot be nil")
	}
	return &FlashResolver{redisClient: redisClient, stockGRPC: stockGRPC}
}

func (r *FlashResolver) Resolve(ctx context.Context, raw []*entity.ItemWithQuantity) ([]*entity.Item, error) {
	if len(raw) == 0 {
		return nil, errors.New("FlashResolver: empty items")
	}

	resolved := make([]*entity.Item, 0, len(raw))
	qtyByID := make(map[string]int32, len(raw))
	var missingIDs []string

	for _, item := range raw {
		qtyByID[item.ID] = item.Quantity

		meta, err := redis.GetFlashMeta(ctx, r.redisClient, flashMetaKeyPrefix+item.ID)
		if err == nil {
			resolved = append(resolved, entity.NewItem(item.ID, meta.Name, item.Quantity, meta.PriceID))
			continue
		}
		if !errors.Is(err, goredis.Nil) {
			// 非"未命中"的 Redis 错误 (网络、连接池) —— 记一行,继续走 gRPC fallback。
			logging.Warnf(ctx, nil, "FlashResolver: redis get failed, id=%s err=%v", item.ID, err)
		}
		missingIDs = append(missingIDs, item.ID)
	}

	if len(missingIDs) == 0 {
		return resolved, nil
	}

	protoItems, err := r.stockGRPC.GetItems(ctx, missingIDs)
	if err != nil {
		return nil, errors.Wrapf(err, "FlashResolver: stock GetItems for ids=%v", missingIDs)
	}
	if len(protoItems) != len(missingIDs) {
		return nil, errors.Errorf("FlashResolver: stock returned %d items for %d ids", len(protoItems), len(missingIDs))
	}

	for _, p := range protoItems {
		resolved = append(resolved, entity.NewItem(p.ID, p.Name, qtyByID[p.ID], p.PriceID))
	}
	return resolved, nil
}
