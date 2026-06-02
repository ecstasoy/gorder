package outbox

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Status 状态机：pending → processing → sent (或 failed)。
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusSent       = "sent"
	StatusFailed     = "failed"
)

// Kind 决定 worker 调 Publisher 的哪个方法发出去。
const (
	KindQueue    = "queue"    // Publisher.Publish (direct queue)
	KindExchange = "exchange" // Publisher.Broadcast (fanout exchange)
	KindDelayed  = "delayed"  // Publisher.PublishDelayed (delayed queue)
)

// CollectionName 是 outbox 在 Mongo 里的 collection 名。固定，不放 viper。
const CollectionName = "order_outbox"

// Record 是一条等待发往 RabbitMQ 的事件。写入与 Order 实体在同一个 Mongo
// 事务里完成；后台 worker 拉取、发送、标记 sent。Payload 是已经 JSON 序列化
// 的事件 body —— worker 直接转发，不再做二次编码。
type Record struct {
	MongoID   bson.ObjectID `bson:"_id"`
	EventID   string             `bson:"event_id"` // 下游用它去重 (at-least-once 投递)
	Dest      string             `bson:"dest"`     // queue 名 或 exchange 名 (Kind=Delayed 时忽略)
	Kind      string             `bson:"kind"`
	Payload   []byte             `bson:"payload"` // 已 JSON 序列化的事件 body
	Status    string             `bson:"status"`
	Attempts  int                `bson:"attempts"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
	SentAt    *time.Time         `bson:"sent_at,omitempty"`
	LastError string             `bson:"last_error,omitempty"`
}
