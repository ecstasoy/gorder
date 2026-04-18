package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/stock/infra/integration"
	"github.com/sirupsen/logrus"
)

type GetItems struct {
	ItemIDs []string
}

type GetItemsHandler decorator.QueryHandler[GetItems, []*orderpb.Item]

type getItemsHandler struct {
	stripeAPI *integration.StripeAPI
}

func NewGetItemsHandler(
	stripeAPI *integration.StripeAPI,
	logger *logrus.Logger,
	metricClient decorator.MetricsClient,
) GetItemsHandler {
	if stripeAPI == nil {
		panic("stripeAPI cannot be nil")
	}
	return decorator.ApplyQueryDecorators[GetItems, []*orderpb.Item](
		getItemsHandler{stripeAPI: stripeAPI},
		logger,
		metricClient,
	)
}

func (g getItemsHandler) Handle(ctx context.Context, query GetItems) ([]*orderpb.Item, error) {
	items := make([]*entity.Item, 0, len(query.ItemIDs))
	for _, id := range query.ItemIDs {
		p, err := g.stripeAPI.GetProductByID(ctx, id)
		if err != nil {
			return nil, err
		}
		// Quantity 在 metadata query 阶段不确定,由上层调用者合并
		items = append(items, entity.NewItem(id, p.Name, 0, p.DefaultPrice.ID))
	}
	return convertor.NewItemConvertor().EntitiesToProtos(items), nil
}
