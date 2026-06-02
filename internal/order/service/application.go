package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/broker"
	grpcClient "github.com/ecstasoy/gorder/common/client"
	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/common/metrics"
	"github.com/ecstasoy/gorder/order/adapters"
	"github.com/ecstasoy/gorder/order/adapters/grpc"
	"github.com/ecstasoy/gorder/order/app"
	"github.com/ecstasoy/gorder/order/app/command"
	"github.com/ecstasoy/gorder/order/app/intake"
	"github.com/ecstasoy/gorder/order/app/query"
	domainsvc "github.com/ecstasoy/gorder/order/domain/service"
	"github.com/ecstasoy/gorder/order/infra/outbox"
	amqp "github.com/rabbitmq/amqp091-go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func NewApplication(ctx context.Context) (app.Application, query.StockService, *goredis.Client, *outbox.MongoOutboxRepo, func()) {
	stockClient, err := grpcClient.NewStockGRPCClient(ctx)
	if err != nil {
		panic(err)
	}
	_, ch, closeCh := broker.Connect(
		viper.GetString("rabbitmq.user"),
		viper.GetString("rabbitmq.password"),
		viper.GetString("rabbitmq.host"),
		viper.GetString("rabbitmq.port"),
	)
	stockGRPC := grpc.NewStockGRPC(stockClient)
	redis.Init()
	redisClient := redis.LocalClient()
	mongoClient := newMongoClient()
	outboxRepo, err := outbox.NewMongoOutboxRepo(ctx, mongoClient)
	if err != nil {
		panic(fmt.Errorf("failed to create outbox repo: %w", err))
	}
	return newApplication(ctx, stockGRPC, redisClient, ch, mongoClient, outboxRepo), stockGRPC, redisClient, outboxRepo, func() {
		_ = grpcClient.CloseStockClient()
		_ = closeCh()
	}
}

func newApplication(_ context.Context, stockGRPC query.StockService, redisClient *goredis.Client, _ *amqp.Channel, mongoClient *mongo.Client, outboxRepo *outbox.MongoOutboxRepo) app.Application {
	orderRepo := adapters.NewOrderRepositoryMongo(mongoClient)
	metricsClient := metrics.NewPrometheusMetricsClient()
	logger := logrus.StandardLogger()

	outboxAppender := &outboxAppenderAdapter{repo: outboxRepo}
	txRunner := &mongoTxRunner{client: mongoClient}

	// ADR-0001 Step 5 + 6: 两个 IntakeOrder 实例 —— 共享 saga 但注入不同 resolver。
	// 这是 "two adapters justify the seam" 的真实落地。
	catalogResolver := intake.NewCatalogResolver(stockGRPC)
	flashResolver := intake.NewFlashResolver(redisClient, stockGRPC)
	intakeSvc := intake.NewIntakeOrder(catalogResolver, stockGRPC, orderRepo, outboxAppender, txRunner)
	flashIntake := intake.NewIntakeOrder(flashResolver, stockGRPC, orderRepo, outboxAppender, txRunner)

	// ADR-0002: lifecycle saga 扩展。Confirm + Cancel 两个 saga 实例。
	confirmSaga := intake.NewConfirmOrder(orderRepo, stockGRPC, outboxAppender, txRunner)
	cancelSaga := intake.NewCancelOrder(orderRepo, stockGRPC, outboxAppender, txRunner)

	return app.Application{
		Commands: app.Commands{
			CreateOrder:      command.NewCreateOrderHandler(intakeSvc, logger, metricsClient),
			UpdateOrder:      command.NewUpdateOrderHandler(orderRepo, logger, metricsClient),
			SetPaymentLink:   command.NewSetPaymentLinkHandler(orderRepo, logger, metricsClient),
			CancelOrder:      command.NewCancelOrderHandler(cancelSaga, logger, metricsClient),
			ConfirmOrder:     command.NewConfirmOrderHandler(confirmSaga, logger, metricsClient),
			CreateFlashOrder: command.NewCreateFlashOrderHandler(flashIntake, logger, metricsClient),
		},
		Queries: app.Queries{
			GetCustomerOrder: query.NewGetCustomerOrderHandler(orderRepo, logrus.StandardLogger(), metricsClient),
		},
	}
}

func newMongoClient() *mongo.Client {
	// replicaSet + directConnection let the v2 driver route writes through
	// the (single) primary of the local single-node rs0 — required by
	// ADR-0001 Step 2 transactions. Production deployments are expected
	// to swap the URI for a real multi-node replica set.
	uri := fmt.Sprintf(
		"mongodb://%s:%s@%s:%d/?authSource=admin&replicaSet=rs0&directConnection=true",
		viper.GetString("mongo.user"),
		viper.GetString("mongo.password"),
		viper.GetString("mongo.host"),
		viper.GetInt("mongo.port"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		panic(fmt.Errorf("failed to connect to MongoDB: %w", err))
	}

	if err = c.Ping(ctx, readpref.Primary()); err != nil {
		panic(fmt.Errorf("failed to ping MongoDB: %w", err))
	}

	logrus.Infof("Successfully connected to MongoDB at %s", uri)
	return c
}

// outboxAppenderAdapter 实现 domainsvc.OutboxAppender —— 把 domain 层的简化
// OutboxRecord 透传到 infra/outbox.Record。adapter 不做映射逻辑,只做类型搬运。
type outboxAppenderAdapter struct {
	repo *outbox.MongoOutboxRepo
}

func (a *outboxAppenderAdapter) Append(ctx context.Context, records []domainsvc.OutboxRecord) error {
	converted := make([]outbox.Record, len(records))
	for i, r := range records {
		converted[i] = outbox.Record{
			EventID: r.EventID,
			Dest:    r.Dest,
			Kind:    r.Kind,
			Payload: r.Payload,
		}
	}
	return a.repo.Append(ctx, converted)
}

// mongoTxRunner 实现 domainsvc.TxRunner —— 把 Mongo session/transaction 藏在
// application 层,domain service 调用时不需要知道 mongo.Client 存在。
type mongoTxRunner struct {
	client *mongo.Client
}

func (t *mongoTxRunner) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	session, err := t.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sCtx context.Context) (any, error) {
		return nil, fn(sCtx)
	})
	return err
}
