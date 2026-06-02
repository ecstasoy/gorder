package service

import "context"

// OutboxRecord 是 domain 层看到的"待发出事件"的最小描述 —— 它不依赖 RabbitMQ /
// MongoDB 类型,让 OrderDomainService 可以独立于 infra 编排。
//
// 适配到 infra 的 outbox.Record 由 service/application.go 中的 adapter 完成。
type OutboxRecord struct {
	EventID string
	Dest    string
	Kind    string // OutboxKindQueue / OutboxKindExchange / OutboxKindDelayed
	Payload []byte
}

// 与 infra/outbox.Kind* 保持值相同 —— adapter 不做映射,直接透传。
const (
	OutboxKindQueue    = "queue"
	OutboxKindExchange = "exchange"
	OutboxKindDelayed  = "delayed"
)

// OutboxAppender 是 OrderDomainService 持有的 outbox 写入能力。
// 实现必须在 caller 给的 ctx 已携带 Mongo session 时,把写入参与 session 的事务。
type OutboxAppender interface {
	Append(ctx context.Context, records []OutboxRecord) error
}

// TxRunner 把"在 Mongo 事务里跑一段逻辑"这件事藏到 application 层 ——
// domain service 调用时不需要知道有 mongo.Client 或 session 存在。
type TxRunner interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}
