package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/logging"
	domain "github.com/ecstasoy/gorder/stock/domain/stock"
	"github.com/ecstasoy/gorder/stock/infra/integration"
	"github.com/sirupsen/logrus"
)

type CheckIfItemsInStock struct {
	Items []*entity.ItemWithQuantity
}

type CheckIfItemsInStockHandler decorator.QueryHandler[CheckIfItemsInStock, []*entity.Item]

type checkIfItemsInStockHandler struct {
	stockRepo domain.Repository
	stripeAPI *integration.StripeAPI
}

func NewCheckIfItemsInStockHandler(
	stockRepo domain.Repository,
	stripeAPI *integration.StripeAPI,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CheckIfItemsInStockHandler {
	if stockRepo == nil {
		panic("stockRepo cannot be nil")
	}
	if stripeAPI == nil {
		panic("stripeAPI cannot be nil")
	}
	return decorator.ApplyQueryDecorators[CheckIfItemsInStock, []*entity.Item](
		checkIfItemsInStockHandler{
			stockRepo: stockRepo,
			stripeAPI: stripeAPI,
		},
		logger,
		metricsClient,
	)
}

// Stub: ProductID → PriceID 映射
var stub = map[string]string{
	// 使用真实的 Stripe PriceID
	"prod_U9k6VcIEwQb83T": "price_1TBQcBBMfcQ39REZYgwPgjcd",
	"prod_U9k6gLFpAGaHQl": "price_1TBQcBBMfcQ39REZYgwPgjcd",
	"item1":               "price_1TBQcBBMfcQ39REZYgwPgjcd",
	"item2":               "price_1TBQcBBMfcQ39REZYgwPgjcd",
	"item3":               "price_1TBQcBBMfcQ39REZYgwPgjcd",
	// 默认价格
	"default": "price_1TBQcBBMfcQ39REZYgwPgjcd",
}

func (c checkIfItemsInStockHandler) Handle(ctx context.Context, query CheckIfItemsInStock) ([]*entity.Item, error) {
	var err error

	var res []*entity.Item
	defer func() {
		f := logrus.Fields{
			"query": query,
			"res":   res,
		}
		if err != nil {
			logging.Errorf(ctx, f, "checkIfItemsInStock err=%v", err)
		} else {
			logging.Infof(ctx, f, "%s", "checkIfItemsInStock success")
		}
	}()

	for _, item := range query.Items {
		p, err := c.stripeAPI.GetProductByID(ctx, item.ID)
		if err != nil {
			return nil, err
		}

		res = append(res, entity.NewItem(item.ID, p.Name, item.Quantity, p.DefaultPrice.ID))
	}

	if err := c.checkStock(ctx, query.Items); err != nil {
		return nil, err
	}

	return res, nil
}

func (c checkIfItemsInStockHandler) checkStock(ctx context.Context, query []*entity.ItemWithQuantity) error {
	var ids []string
	for _, item := range query {
		ids = append(ids, item.ID)
	}
	records, err := c.stockRepo.GetStock(ctx, ids)
	if err != nil {
		logrus.Errorf("error fetching stock records: %v", err)
		return err
	}
	var idQuantityMap = make(map[string]int32)
	for _, record := range records {
		idQuantityMap[record.ID] += record.Quantity
	}
	var (
		ok       = true
		failedOn []struct {
			ID   string
			Want int32
			Have int32
		}
	)
	for _, item := range query {
		if item.Quantity > idQuantityMap[item.ID] {
			ok = false
			failedOn = append(failedOn, struct {
				ID   string
				Want int32
				Have int32
			}{ID: item.ID, Want: item.Quantity, Have: idQuantityMap[item.ID]})
		}
	}
	if ok {
		return c.stockRepo.UpdateStock(ctx, query, func(
			ctx context.Context,
			existing []*entity.ItemWithQuantity,
			query []*entity.ItemWithQuantity,
		) ([]*entity.ItemWithQuantity, error) {
			var newItems []*entity.ItemWithQuantity
			for _, e := range existing {
				for _, q := range query {
					if e.ID == q.ID {
						iq, err := entity.NewValidItemWithQuantity(e.ID, e.Quantity-q.Quantity)
						if err != nil {
							return nil, err
						}
						newItems = append(newItems, iq)
					}
				}
			}
			return newItems, nil
		})
	}
	return domain.ExceedStockError{FailedOn: failedOn}
}

func getStubPriceID(id string) string {
	priceId, ok := stub[id]
	if !ok {
		logrus.Warnf("No price mapping for product %s, using default", id)
		priceId = stub["default"]
	}
	return priceId
}
