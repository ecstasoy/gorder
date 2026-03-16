package ports

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/stockpb"
	"github.com/ecstasoy/gorder/stock/app"
	"github.com/ecstasoy/gorder/stock/app/query"
	"github.com/sirupsen/logrus"
)

type GRPCServer struct {
	app app.Application
}

func NewGRPCServer(app app.Application) *GRPCServer {
	return &GRPCServer{app: app}
}

func (G GRPCServer) GetItems(ctx context.Context, request *stockpb.GetItemsRequest) (*stockpb.GetItemsResponse, error) {
	items, err := G.app.Queries.GetItems.Handle(ctx, query.GetItems{
		ItemIDs: request.ItemIDs,
	})
	if err != nil {
		logrus.Errorf("error handling GetItems query: %v", err)
		return nil, err
	}
	return &stockpb.GetItemsResponse{Items: items}, nil
}

func (G GRPCServer) CheckIfItemsInStock(ctx context.Context, request *stockpb.CheckIfItemsInStockRequest) (*stockpb.CheckIfItemsInStockResponse, error) {
	items, err := G.app.Queries.CheckIfItemsInStock.Handle(ctx, query.CheckIfItemsInStock{
		Items: request.Items,
	})
	if err != nil {
		logrus.Errorf("error handling CheckIfItemsInStock query: %v", err)
		return nil, err
	}
	return &stockpb.CheckIfItemsInStockResponse{
		InStock: true,
		Items:   items,
	}, nil
}
