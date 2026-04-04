package adapters

import (
	"context"
	"sync"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/sirupsen/logrus"
)

type MemoryStockRepository struct {
	lock  *sync.RWMutex
	store map[string]*entity.Item
}

func (m MemoryStockRepository) GetStock(ctx context.Context, ids []string) ([]*entity.ItemWithQuantity, error) {
	//TODO implement me
	panic("implement me")
}

var stub = map[string]*entity.Item{
	"item": {
		ID:       "foo_item",
		Name:     "stub_item",
		Quantity: 10000,
		PriceID:  "stub_item_price_id",
	},
	"item1": {
		ID:       "item1",
		Name:     "stub_item1",
		Quantity: 10000,
		PriceID:  "stub_item1_price_id",
	},
	"item2": {
		ID:       "item2",
		Name:     "stub_item2",
		Quantity: 10000,
		PriceID:  "stub_item2_price_id",
	},
	"item3": {
		ID:       "item3",
		Name:     "stub_item3",
		Quantity: 10000,
		PriceID:  "stub_item3_price_id",
	},
}

func NewMemoryStockRepository() *MemoryStockRepository {
	return &MemoryStockRepository{
		lock:  &sync.RWMutex{},
		store: stub,
	}
}

func (m MemoryStockRepository) GetItems(ctx context.Context, ids []string) ([]*entity.Item, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	var (
		res     []*orderpb.Item
		missing []string
	)
	for _, id := range ids {
		if item, exist := m.store[id]; exist {
			res = append(res, convertor.NewItemConvertor().EntityToProto(item))
		} else {
			missing = append(missing, id)
		}
	}
	if len(res) > 0 {
		logrus.WithFields(logrus.Fields{
			"ids":     ids,
			"result":  res,
			"missing": missing,
			"store":   m.store,
		}).Debugf("GetItems: returning %v", res)
		return convertor.NewItemConvertor().ProtosToEntities(res), nil
	}
	return nil, domain.NotFoundError{Missing: missing}
}
