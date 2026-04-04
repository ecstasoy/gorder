package ports

import (
	"context"

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

func (G GRPCServer) RestoreStock(ctx context.Context, request *stockpb.RestoreStockRequest) (*stockpb.RestoreStockResponse, error) {
	_, span := tracing.Start(ctx, "grpc.RestoreStock")
	defer span.End()

	_, err := G.app.Commands.RestoreStock.Handle(ctx, command.RestoreStock{
		Items: convertor.NewItemWithQuantityConvertor().ProtosToEntities(request.Items),
	})
	if err != nil {
		logrus.Errorf("error handling RestoreStock command: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.RestoreStockResponse{}, nil
}

func (G GRPCServer) CheckIfItemsInStock(ctx context.Context, request *stockpb.CheckIfItemsInStockRequest) (*stockpb.CheckIfItemsInStockResponse, error) {
	_, span := tracing.Start(ctx, "grpc.CheckIfItemsInStock")
	defer span.End()

	items, err := G.app.Queries.CheckIfItemsInStock.Handle(ctx, query.CheckIfItemsInStock{
		Items: convertor.NewItemWithQuantityConvertor().ProtosToEntities(request.Items),
	})
	if err != nil {
		logrus.Errorf("error handling CheckIfItemsInStock query: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &stockpb.CheckIfItemsInStockResponse{
		InStock: true,
		Items:   convertor.NewItemConvertor().EntitiesToProtos(items),
	}, nil
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
