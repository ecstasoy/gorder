package adapters

import (
	"context"

	_ "github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	dbName   = viper.GetString("mongo.db-name")
	collName = viper.GetString("mongo.coll-name")
)

type OrderRepositoryMongo struct {
	db *mongo.Client
}

func NewOrderRepositoryMongo(db *mongo.Client) *OrderRepositoryMongo {
	return &OrderRepositoryMongo{db: db}
}

func (r *OrderRepositoryMongo) collection() *mongo.Collection {
	return r.db.Database(dbName).Collection(collName)
}

type orderModel struct {
	MongoID     primitive.ObjectID `bson:"_id"`
	ID          string             `bson:"id"`
	CustomerID  string             `bson:"customer_id"`
	Status      string             `bson:"status"`
	PaymentLink string             `bson:"payment_link"`
	Items       []*entity.Item     `bson:"items"`
	// ADR-0004 — flash sale 活动归属;常规订单为空字符串。
	ActivityID string `bson:"activity_id,omitempty"`
}

func (r *OrderRepositoryMongo) Create(ctx context.Context, order *domain.Order) (created *domain.Order, err error) {
	_, deferLog := logging.WhenRequest(ctx, "OrderRepositoryMongo.Create", map[string]any{"order": order})
	defer deferLog(created, &err)

	// ADR-0001 Step 5: saga 在 Reserve 之前生成 OrderID,Create 必须尊重它,
	// 否则 saga 持有的 ID (用作 reservation 幂等键) 会和 Mongo 实际写入的
	// _id 不一致 —— 补偿调 Release 时 stock 找不到 row。
	var mongoID primitive.ObjectID
	if order.ID != "" {
		mongoID, err = primitive.ObjectIDFromHex(order.ID)
		if err != nil {
			return nil, errors.Wrap(err, "OrderRepositoryMongo.Create: invalid pre-set ID hex")
		}
	} else {
		mongoID = primitive.NewObjectID()
	}
	write := r.marshalToModel(order)
	write.MongoID = mongoID

	_, err = r.collection().InsertOne(ctx, write)
	if err != nil {
		return nil, err
	}

	created = order
	created.ID = mongoID.Hex()
	return created, nil
}

func (r *OrderRepositoryMongo) Get(ctx context.Context, id, customerID string) (got *domain.Order, err error) {
	_, deferLog := logging.WhenRequest(ctx, "OrderRepositoryMongo.Get", map[string]any{
		"id":          id,
		"customer_id": customerID,
	})
	defer func() {
		deferLog(got, &err)
	}()

	read := &orderModel{}
	mongoID, _ := primitive.ObjectIDFromHex(id)
	cond := bson.M{"_id": mongoID}
	err = r.collection().FindOne(ctx, cond).Decode(&read)
	if err != nil {
		return nil, err
	}
	if read == nil {
		return nil, &domain.NotFoundError{OrderID: id}
	}

	got = r.unmarshal(read)
	return got, nil
}

// Update first gets the order by id and customerID, then applies the updateFunc to the order, and finally saves the updated order back to the database.
func (r *OrderRepositoryMongo) Update(ctx context.Context, o *domain.Order, updateFunc func(context.Context, *domain.Order) (*domain.Order, error)) (err error) {
	_, deferLog := logging.WhenRequest(ctx, "OrderRepositoryMongo.Get", map[string]any{
		"order": o,
	})
	defer func() {
		deferLog(nil, &err)
	}()

	if o == nil {
		panic("got nil order")
	}

	session, err := r.db.StartSession()
	if err != nil {
		return
	}
	defer session.EndSession(ctx)

	// v2 driver: collection 操作必须收到 sessionContext (WithTransaction 的 callback ctx)
	// 才会参与事务；用普通 ctx 会让 read/write 静默地脱离事务，留下并发竞争窗口。
	_, err = session.WithTransaction(ctx, func(sCtx context.Context) (any, error) {
		oldOrder, err := r.Get(sCtx, o.ID, o.CustomerID)
		if err != nil {
			return nil, err
		}
		updated, err := updateFunc(sCtx, oldOrder)
		if err != nil {
			return nil, err
		}
		logrus.Infof("update || oldOrder=%+v || updated=%+v", oldOrder, updated)
		mongoID, _ := primitive.ObjectIDFromHex(oldOrder.ID)
		_, err = r.collection().UpdateOne(
			sCtx,
			bson.M{"_id": mongoID, "customer_id": oldOrder.CustomerID},
			bson.M{"$set": bson.M{
				"status":       updated.Status.String(), // orderpb.OrderStatus → string
				"payment_link": updated.PaymentLink,
			}},
		)
		return nil, err
	})

	return
}

func (r *OrderRepositoryMongo) marshalToModel(order *domain.Order) *orderModel {
	return &orderModel{
		MongoID:     primitive.NewObjectID(),
		ID:          order.ID,
		CustomerID:  order.CustomerID,
		Status:      order.Status.String(),
		PaymentLink: order.PaymentLink,
		Items:       order.Items,
		ActivityID:  order.ActivityID,
	}
}

func (r *OrderRepositoryMongo) unmarshal(m *orderModel) *domain.Order {
	status := orderpb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	if statusValue, ok := orderpb.OrderStatus_value[m.Status]; ok {
		status = orderpb.OrderStatus(statusValue)
	}

	return &domain.Order{
		ID:          m.MongoID.Hex(),
		CustomerID:  m.CustomerID,
		Status:      status,
		PaymentLink: m.PaymentLink,
		Items:       m.Items,
		ActivityID:  m.ActivityID,
	}
}
