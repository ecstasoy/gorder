package adapters

import (
	"context"

	_ "github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	domain "github.com/ecstasoy/gorder/order/domain/order"
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
}

func (r *OrderRepositoryMongo) Create(ctx context.Context, order *domain.Order) (created *domain.Order, err error) {
	_, deferLog := logging.WhenRequest(ctx, "OrderRepositoryMongo.Create", map[string]any{"order": order})
	defer deferLog(created, &err)

	mongoID := primitive.NewObjectID()
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

	if err = session.StartTransaction(); err != nil {
		return err
	}
	defer func() {
		if err == nil {
			_ = session.CommitTransaction(ctx)
		} else {
			_ = session.AbortTransaction(ctx)
		}
	}()

	oldOrder, err := r.Get(ctx, o.ID, o.CustomerID)
	if err != nil {
		return
	}
	updated, err := updateFunc(ctx, oldOrder)
	if err != nil {
		return
	}
	logrus.Infof("update || oldOrder=%+v || updated=%+v", oldOrder, updated)
	mongoID, _ := primitive.ObjectIDFromHex(oldOrder.ID)
	_, err = r.collection().UpdateOne(
		ctx,
		bson.M{"_id": mongoID, "customer_id": oldOrder.CustomerID},
		bson.M{"$set": bson.M{
			"status":       updated.Status.String(), // orderpb.OrderStatus → string
			"payment_link": updated.PaymentLink,
		}},
	)

	if err != nil {
		return
	}

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
	}
}
