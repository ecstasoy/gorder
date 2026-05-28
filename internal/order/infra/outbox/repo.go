package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoOutboxRepo 把 Mongo 操作细节藏在 outbox 包内 —— caller 只看到
// Append / ClaimNext / MarkSent / MarkFailed 四个动作。
type MongoOutboxRepo struct {
	client *mongo.Client
}

func NewMongoOutboxRepo(ctx context.Context, client *mongo.Client) (*MongoOutboxRepo, error) {
	r := &MongoOutboxRepo{client: client}
	if err := r.ensureIndexes(ctx); err != nil {
		return nil, fmt.Errorf("outbox: ensure indexes: %w", err)
	}
	return r, nil
}

func (r *MongoOutboxRepo) collection() *mongo.Collection {
	return r.client.Database(viper.GetString("mongo.db-name")).Collection(CollectionName)
}

func (r *MongoOutboxRepo) ensureIndexes(ctx context.Context) error {
	_, err := r.collection().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "event_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	})
	return err
}

// Append 写入若干 outbox 记录。caller 在 Mongo session 内调用时（ctx 携带 session），
// 写入参与 caller 的 transaction；否则独立写入。Status 强制设为 pending、时间戳由本方法填。
func (r *MongoOutboxRepo) Append(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	now := time.Now().UTC()
	docs := make([]any, 0, len(records))
	for i := range records {
		rec := &records[i]
		if rec.MongoID.IsZero() {
			rec.MongoID = primitive.NewObjectID()
		}
		if rec.EventID == "" {
			return fmt.Errorf("outbox: record %d missing event_id", i)
		}
		switch rec.Kind {
		case KindQueue, KindExchange, KindDelayed:
		default:
			return fmt.Errorf("outbox: record %d invalid kind %q", i, rec.Kind)
		}
		rec.Status = StatusPending
		rec.CreatedAt = now
		rec.UpdatedAt = now
		docs = append(docs, rec)
	}
	_, err := r.collection().InsertMany(ctx, docs)
	return err
}

// ClaimNext 原子地拿一条 pending 记录并把它标为 processing，返回更新后的 record。
// 没有可拿的记录时返回 (nil, nil)。多 worker 实例同时调用，原子性靠 Mongo 保证。
func (r *MongoOutboxRepo) ClaimNext(ctx context.Context) (*Record, error) {
	now := time.Now().UTC()
	filter := bson.M{"status": StatusPending}
	update := bson.M{
		"$set": bson.M{"status": StatusProcessing, "updated_at": now},
		"$inc": bson.M{"attempts": 1},
	}
	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.After).
		SetSort(bson.D{{Key: "created_at", Value: 1}})

	var rec Record
	err := r.collection().FindOneAndUpdate(ctx, filter, update, opts).Decode(&rec)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

func (r *MongoOutboxRepo) MarkSent(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now().UTC()
	_, err := r.collection().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"status":     StatusSent,
			"sent_at":    now,
			"updated_at": now,
		},
	})
	return err
}

// MarkFailed 退回 pending (等下次 tick 重试) 或终止为 failed (达上限)。
// 终态不删 record —— 留给运维 / 对账任务处理。
func (r *MongoOutboxRepo) MarkFailed(ctx context.Context, id primitive.ObjectID, errMsg string, terminal bool) error {
	now := time.Now().UTC()
	status := StatusPending
	if terminal {
		status = StatusFailed
	}
	_, err := r.collection().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"status":     status,
			"last_error": errMsg,
			"updated_at": now,
		},
	})
	return err
}
