package broker

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DomainEvent 承载一次事件发布。Dest 在 Publish 时被视为 queue 名（direct）、
// 在 Broadcast 时被视为 exchange 名（fanout）、在 PublishDelayed 时被忽略
// （固定使用 OrderPaymentDelayQueue，TTL 由队列声明决定）。
type DomainEvent struct {
	Dest string
	Data any
}

// Publisher 把 RabbitMQ 细节隔离在 adapter 里，让 application/domain 层
// 只依赖"发一个事件"的抽象，未来换 Kafka/NATS 时只改此包的实现。
type Publisher interface {
	Publish(ctx context.Context, event DomainEvent) error
	Broadcast(ctx context.Context, event DomainEvent) error
	PublishDelayed(ctx context.Context, event DomainEvent) error
}

type RabbitMQPublisher struct {
	channel *amqp.Channel
}

func NewRabbitMQPublisher(ch *amqp.Channel) *RabbitMQPublisher {
	return &RabbitMQPublisher{channel: ch}
}

func (r *RabbitMQPublisher) Publish(ctx context.Context, event DomainEvent) error {
	return publishEvent(ctx, PublishEventReq{
		Channel:  r.channel,
		Routing:  Direct,
		Queue:    event.Dest,
		Exchange: "",
		Body:     event.Data,
	})
}

func (r *RabbitMQPublisher) Broadcast(ctx context.Context, event DomainEvent) error {
	return publishEvent(ctx, PublishEventReq{
		Channel:  r.channel,
		Routing:  FanOut,
		Queue:    "",
		Exchange: event.Dest,
		Body:     event.Data,
	})
}

func (r *RabbitMQPublisher) PublishDelayed(ctx context.Context, event DomainEvent) error {
	return publishToDelayQueue(ctx, r.channel, event.Data)
}
