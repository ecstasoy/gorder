package grpc

import (
	"context"

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

func (s StockGRPC) GetItems(ctx context.Context, itemIDs []string) (items []*orderpb.Item, err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.GetItems", items)
	defer dLog(items, &err)

	resp, err := s.client.GetItems(ctx, &stockpb.GetItemsRequest{ItemIDs: itemIDs})
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
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

func (s StockGRPC) Reserve(ctx context.Context, orderID string, items []*orderpb.ItemWithQuantity) (err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.Reserve", map[string]any{"order_id": orderID, "items": items})
	defer dLog(nil, &err)

	_, err = s.client.Reserve(ctx, &stockpb.ReserveRequest{OrderID: orderID, Items: items})
	return err
}

func (s StockGRPC) Confirm(ctx context.Context, orderID string) (err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.Confirm", map[string]any{"order_id": orderID})
	defer dLog(nil, &err)

	_, err = s.client.Confirm(ctx, &stockpb.ConfirmRequest{OrderID: orderID})
	return err
}

func (s StockGRPC) Release(ctx context.Context, orderID string) (err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.Release", map[string]any{"order_id": orderID})
	defer dLog(nil, &err)

	_, err = s.client.Release(ctx, &stockpb.ReleaseRequest{OrderID: orderID})
	return err
}

// ---- ADR-0004 activity calls ----

// ActivityInfo 是 order 侧需要的活动元数据子集。
type ActivityInfo struct {
	ActivityID string
	Name       string
	ProductID  string
	TotalStock int32
	Status     string
	WarmupDone bool
}

func (s StockGRPC) CreateActivity(ctx context.Context, name, productID string, totalStock int32, startUnix, endUnix int64) (activityID string, err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.CreateActivity", map[string]any{"name": name, "product": productID})
	defer dLog(nil, &err)

	resp, err := s.client.CreateActivity(ctx, &stockpb.CreateActivityRequest{
		Name:          name,
		ProductID:     productID,
		TotalStock:    totalStock,
		StartTimeUnix: startUnix,
		EndTimeUnix:   endUnix,
	})
	if err != nil {
		return "", err
	}
	return resp.ActivityID, nil
}

func (s StockGRPC) WarmUpActivity(ctx context.Context, activityID string) (skipped bool, err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.WarmUpActivity", map[string]any{"activity_id": activityID})
	defer dLog(nil, &err)

	resp, err := s.client.WarmUpActivity(ctx, &stockpb.WarmUpActivityRequest{ActivityID: activityID})
	if err != nil {
		return false, err
	}
	return resp.Skipped, nil
}

func (s StockGRPC) GetActivity(ctx context.Context, activityID string) (info *ActivityInfo, err error) {
	_, dLog := logging.WhenRequest(ctx, "StockGRPC.GetActivity", map[string]any{"activity_id": activityID})
	defer dLog(nil, &err)

	resp, err := s.client.GetActivity(ctx, &stockpb.GetActivityRequest{ActivityID: activityID})
	if err != nil {
		return nil, err
	}
	return &ActivityInfo{
		ActivityID: resp.ActivityID,
		Name:       resp.Name,
		ProductID:  resp.ProductID,
		TotalStock: resp.TotalStock,
		Status:     resp.Status,
		WarmupDone: resp.WarmupDone,
	}, nil
}
