package grpc

import (
	"context"
	"errors"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/genproto/stockpb"
	"github.com/ecstasoy/gorder/common/logging"
)

type StockGRPC struct {
	client stockpb.StockServiceClient
}

func NewStockGRPC(client stockpb.StockServiceClient) *StockGRPC {
	return &StockGRPC{client: client}
}

func (s StockGRPC) CheckIfItemsInStock(ctx context.Context, items []*orderpb.ItemWithQuantity) (resp *stockpb.CheckIfItemsInStockResponse, err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.CheckIfItemsInStock", items)
	defer dLog(resp, &err)

	if items == nil {
		return nil, errors.New("grpc items cannot be nil")
	}
	return s.client.CheckIfItemsInStock(ctx, &stockpb.CheckIfItemsInStockRequest{Items: items})
}

func (s StockGRPC) GetItems(ctx context.Context, itemIDs []string) (items []*orderpb.Item, err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.GetItems", items)
	defer dLog(items, &err)

	resp, err := s.client.GetItems(ctx, &stockpb.GetItemsRequest{ItemIDs: itemIDs})
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (s StockGRPC) RestoreStock(ctx context.Context, items []*orderpb.ItemWithQuantity) (err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.RestoreStock", items)
	defer dLog(nil, &err)

	_, err = s.client.RestoreStock(ctx, &stockpb.RestoreStockRequest{Items: items})
	return err
}

func (s StockGRPC) WarmUpFlashStock(ctx context.Context, items []*orderpb.ItemWithQuantity, ttlSeconds int64) (err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.WarmUpFlashStock", items)
	defer dLog(nil, &err)

	_, err = s.client.WarmUpFlashStock(ctx, &stockpb.WarmUpFlashStockRequest{
		Items:      items,
		TTLSeconds: ttlSeconds,
	})

	return err
}

func (s StockGRPC) DeductStock(ctx context.Context, items []*orderpb.ItemWithQuantity) (err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.DeductStock", items)
	defer dLog(nil, &err)

	_, err = s.client.DeductStock(ctx, &stockpb.DeductStockRequest{Items: items})
	return err
}
