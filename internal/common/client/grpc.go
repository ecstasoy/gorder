package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ecstasoy/gorder/common/discovery"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/genproto/stockpb"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	stockClientOnce sync.Once
	stockClient     stockpb.StockServiceClient
	stockConn       *grpc.ClientConn
	stockClientErr  error

	orderClientOnce sync.Once
	orderClient     orderpb.OrderServiceClient
	orderConn       *grpc.ClientConn
	orderClientErr  error
)

func NewStockGRPCClient(ctx context.Context) (stockpb.StockServiceClient, error) {
	stockClientOnce.Do(func() {
		serviceName := viper.GetString("stock.service-name")
		timeout := viper.GetDuration("dial-grpc-timeout")
		if timeout <= 0 {
			timeout = 10 * time.Second
		}

		grpcAddr, err := waitForServiceAddr(ctx, serviceName, timeout)
		if err != nil {
			stockClientErr = err
			return
		}

		opts, err := grpcDialOpts(grpcAddr)
		if err != nil {
			stockClientErr = err
			return
		}

		conn, err := grpc.NewClient(grpcAddr, opts...)
		if err != nil {
			stockClientErr = err
			return
		}

		stockConn = conn
		stockClient = stockpb.NewStockServiceClient(conn)
		logrus.Infof("Stock gRPC client initialized: %s", grpcAddr)
	})

	return stockClient, stockClientErr
}

func CloseStockClient() error {
	if stockConn != nil {
		return stockConn.Close()
	}
	return nil
}

func NewOrderGRPCClient(ctx context.Context) (orderpb.OrderServiceClient, error) {
	orderClientOnce.Do(func() {
		serviceName := viper.GetString("order.service-name")
		timeout := viper.GetDuration("dial-grpc-timeout")
		if timeout <= 0 {
			timeout = 10 * time.Second
		}

		grpcAddr, err := waitForServiceAddr(ctx, serviceName, timeout)
		if err != nil {
			orderClientErr = err
			return
		}

		opts, err := grpcDialOpts(grpcAddr)
		if err != nil {
			orderClientErr = err
			return
		}

		conn, err := grpc.NewClient(grpcAddr, opts...)
		if err != nil {
			orderClientErr = err
			return
		}

		orderConn = conn
		orderClient = orderpb.NewOrderServiceClient(conn)
		logrus.Infof("Order gRPC client initialized: %s", grpcAddr)
	})

	return orderClient, orderClientErr
}

func CloseOrderClient() error {
	if orderConn != nil {
		return orderConn.Close()
	}
	return nil
}

func grpcDialOpts(_ string) ([]grpc.DialOption, error) {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}, nil
}

func waitForServiceAddr(ctx context.Context, serviceName string, timeout time.Duration) (string, error) {
	logrus.Infof("waiting for service discovery: %s, timeout: %s", serviceName, timeout)

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		addr, err := discovery.GetServiceAddr(deadlineCtx, serviceName)
		if err == nil && addr != "" {
			logrus.Infof("service discovered: %s -> %s", serviceName, addr)
			return addr, nil
		}

		select {
		case <-deadlineCtx.Done():
			if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("service %s not found in consul within %s", serviceName, timeout)
			}
			return "", deadlineCtx.Err()
		case <-ticker.C:
		}
	}
}
