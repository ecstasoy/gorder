package ports

import (
	"context"
	"time"

	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/genproto/stockpb"
	"github.com/ecstasoy/gorder/common/tracing"
	"github.com/ecstasoy/gorder/stock/app"
	"github.com/ecstasoy/gorder/stock/app/command"
	"github.com/ecstasoy/gorder/stock/app/query"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GRPCServer struct {
	app app.Application
}

func NewGRPCServer(app app.Application) *GRPCServer {
	return &GRPCServer{app: app}
}

func (G GRPCServer) GetItems(ctx context.Context, request *stockpb.GetItemsRequest) (*stockpb.GetItemsResponse, error) {
	_, span := tracing.Start(ctx, "grpc.GetItems")
	defer span.End()

	items, err := G.app.Queries.GetItems.Handle(ctx, query.GetItems{
		ItemIDs: request.ItemIDs,
	})
	if err != nil {
		logrus.Errorf("error handling GetItems query: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.GetItemsResponse{Items: items}, nil
}

func (G GRPCServer) WarmUpFlashStock(ctx context.Context, request *stockpb.WarmUpFlashStockRequest) (*stockpb.WarmUpFlashStockResponse, error) {
	_, span := tracing.Start(ctx, "grpc.WarmUpFlashStock")
	defer span.End()

	_, err := G.app.Commands.WarmUpFlashStock.Handle(ctx, command.WarmUpFlashStock{
		Items:      convertor.NewItemWithQuantityConvertor().ProtosToEntities(request.Items),
		TTLSeconds: request.TTLSeconds,
	})
	if err != nil {
		logrus.Errorf("error handling WarmUpFlashStock command: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.WarmUpFlashStockResponse{}, nil
}

// Reserve / Confirm / Release — ADR-0001 Step 4.
// OrderID 是幂等键;domain 错误类型 (NotFoundError / ConflictError /
// InsufficientStockError) 暂时统一映射为 codes.Internal。

func (G GRPCServer) Reserve(ctx context.Context, request *stockpb.ReserveRequest) (*stockpb.ReserveResponse, error) {
	_, span := tracing.Start(ctx, "grpc.Reserve")
	defer span.End()

	_, err := G.app.Commands.ReserveStock.Handle(ctx, command.ReserveStock{
		OrderID: request.OrderID,
		Items:   convertor.NewItemWithQuantityConvertor().ProtosToEntities(request.Items),
	})
	if err != nil {
		logrus.Errorf("error handling Reserve command: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.ReserveResponse{}, nil
}

func (G GRPCServer) Confirm(ctx context.Context, request *stockpb.ConfirmRequest) (*stockpb.ConfirmResponse, error) {
	_, span := tracing.Start(ctx, "grpc.Confirm")
	defer span.End()

	_, err := G.app.Commands.ConfirmStock.Handle(ctx, command.ConfirmStock{
		OrderID: request.OrderID,
	})
	if err != nil {
		logrus.Errorf("error handling Confirm command: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.ConfirmResponse{}, nil
}

func (G GRPCServer) Release(ctx context.Context, request *stockpb.ReleaseRequest) (*stockpb.ReleaseResponse, error) {
	_, span := tracing.Start(ctx, "grpc.Release")
	defer span.End()

	_, err := G.app.Commands.ReleaseStock.Handle(ctx, command.ReleaseStock{
		OrderID: request.OrderID,
	})
	if err != nil {
		logrus.Errorf("error handling Release command: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.ReleaseResponse{}, nil
}

// ---- ADR-0004 Activity RPCs ----

func (G GRPCServer) CreateActivity(ctx context.Context, request *stockpb.CreateActivityRequest) (*stockpb.CreateActivityResponse, error) {
	_, span := tracing.Start(ctx, "grpc.CreateActivity")
	defer span.End()

	res, err := G.app.Commands.CreateActivity.Handle(ctx, command.CreateActivity{
		Name:       request.Name,
		ProductID:  request.ProductID,
		TotalStock: request.TotalStock,
		StartTime:  time.Unix(request.StartTimeUnix, 0).UTC(),
		EndTime:    time.Unix(request.EndTimeUnix, 0).UTC(),
	})
	if err != nil {
		logrus.Errorf("CreateActivity error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.CreateActivityResponse{ActivityID: res.ActivityID}, nil
}

func (G GRPCServer) WarmUpActivity(ctx context.Context, request *stockpb.WarmUpActivityRequest) (*stockpb.WarmUpActivityResponse, error) {
	_, span := tracing.Start(ctx, "grpc.WarmUpActivity")
	defer span.End()

	res, err := G.app.Commands.WarmUpActivity.Handle(ctx, command.WarmUpActivity{
		ActivityID: request.ActivityID,
	})
	if err != nil {
		logrus.Errorf("WarmUpActivity error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.WarmUpActivityResponse{Skipped: res.Skipped}, nil
}

func (G GRPCServer) GetActivity(ctx context.Context, request *stockpb.GetActivityRequest) (*stockpb.GetActivityResponse, error) {
	_, span := tracing.Start(ctx, "grpc.GetActivity")
	defer span.End()

	a, err := G.app.Queries.GetActivity.Handle(ctx, query.GetActivity{ActivityID: request.ActivityID})
	if err != nil {
		logrus.Errorf("GetActivity error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.GetActivityResponse{
		ActivityID:    a.ID,
		Name:          a.Name,
		ProductID:     a.ProductID,
		TotalStock:    a.TotalStock,
		StartTimeUnix: a.StartTime.Unix(),
		EndTimeUnix:   a.EndTime.Unix(),
		Status:        string(a.Status),
		WarmupDone:    a.WarmupDone,
	}, nil
}
