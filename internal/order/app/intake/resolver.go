package intake

import (
	"context"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/order/app/query"
	"github.com/pkg/errors"
)

// ItemResolver 是 intake saga 的商品元数据解析口 —— 把客户端送入的
// (item_id, quantity) 翻译成带 name + priceID 的完整 Item。
//
// 两个 adapter (CatalogResolver / FlashResolver,后者 Step 6 加) 内部 fallback
// 链各自封装,saga 完全不感知 (ADR-0001 Q7)。
type ItemResolver interface {
	Resolve(ctx context.Context, raw []*entity.ItemWithQuantity) ([]*entity.Item, error)
}

// CatalogResolver 走 stock 服务的 GetItems —— 解析常规目录商品的元数据。
// 与 FlashResolver 的区别:不走 Redis 热数据,直接 gRPC。库存校验由 saga
// 的 Reserve 步骤负责 (不再调 CheckIfItemsInStock)。
type CatalogResolver struct {
	stockGRPC query.StockService
}

func NewCatalogResolver(stockGRPC query.StockService) *CatalogResolver {
	if stockGRPC == nil {
		panic("CatalogResolver: stockGRPC cannot be nil")
	}
	return &CatalogResolver{stockGRPC: stockGRPC}
}

func (r *CatalogResolver) Resolve(ctx context.Context, raw []*entity.ItemWithQuantity) ([]*entity.Item, error) {
	if len(raw) == 0 {
		return nil, errors.New("CatalogResolver: empty items")
	}

	qtyByID := make(map[string]int32, len(raw))
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		qtyByID[item.ID] = item.Quantity
		ids = append(ids, item.ID)
	}

	protoItems, err := r.stockGRPC.GetItems(ctx, ids)
	if err != nil {
		return nil, errors.Wrap(err, "CatalogResolver: stock GetItems")
	}
	if len(protoItems) != len(ids) {
		return nil, errors.Errorf("CatalogResolver: stock returned %d items for %d ids", len(protoItems), len(ids))
	}

	itemConv := convertor.NewItemConvertor()
	resolved := make([]*entity.Item, 0, len(protoItems))
	for _, p := range protoItems {
		e := itemConv.ProtoToEntity(p)
		e.Quantity = qtyByID[p.ID] // 用客户端送入的 quantity 覆盖 (proto 里 quantity 是商品总库存)
		resolved = append(resolved, e)
	}
	return resolved, nil
}
