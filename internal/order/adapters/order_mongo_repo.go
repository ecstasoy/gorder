package adapters

import (
	"context"
	"time"

	_ "github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/ecstasoy/gorder/order/entity"
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
	defer r.logWithTag("create", err, order, created)

	mongoID := primitive.NewObjectID()
	write := r.marchalToModel(order)
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
	defer r.logWithTag("get", err, nil, got)
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
	defer r.logWithTag("update", err, o, nil)
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
	updated, err := updateFunc(ctx, o)
	if err != nil {
		return
	}
	logrus.Infof("update || oldOrder=%+v || updated=%+v", oldOrder, updated)
	mongoID, _ := primitive.ObjectIDFromHex(oldOrder.ID)
	res, err := r.collection().UpdateOne(
		ctx,
		bson.M{"_id": mongoID, "customer_id": oldOrder.CustomerID},
		bson.M{"$set": bson.M{
			"status":       updated.Status.String(),  // orderpb.OrderStatus → string
			"payment_link": updated.PaymentLink,
		}},
	)
	if err != nil {
		return
	}
	r.logWithTag("finish_update", err, o, res)
	return
}

func (r *OrderRepositoryMongo) logWithTag(tag string, err error, input *domain.Order, result interface{}) {
	l := logrus.WithFields(logrus.Fields{
		"tag":            "order_repository_mongo",
		"input_order":    input,
		"performed_time": time.Now().Unix(),
		"err":            err,
		"result":         result,
	})
	if err != nil {
		l.Infof("%s_fail", tag)
	} else {
		l.Infof("%s_success", tag)
	}
}

func (r *OrderRepositoryMongo) marchalToModel(order *domain.Order) *orderModel {
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
