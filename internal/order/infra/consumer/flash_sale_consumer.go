package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/common/metrics"
	"github.com/ecstasoy/gorder/order/app/command"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

const (
	flashResultTTL       = 24 * time.Hour
	flashStockKeyPrefix  = "flash:stock:"
	flashOnceKeyPrefix   = "flash:once:"
	flashResultKeyPrefix = "flash:result:"
)

type flashResultPayload struct {
	Status  string `json:"status"`
	OrderID string `json:"order_id,omitempty"`
	Message string `json:"message,omitempty"`
}

const (
	flashSaleOrderPrefetch = 100
	flashSaleOrderWorkers  = 20
)

func (c *Consumer) ListenFlashSaleOrders(ch *amqp.Channel) {
	// Prefetch 让 broker 一次 push 多条消息到 client 端 buffer,多个 worker 才能并行抢
	if err := ch.Qos(flashSaleOrderPrefetch, 0, false); err != nil {
		logrus.Fatal(fmt.Errorf("flash sale consumer: set QoS: %w", err))
	}

	q, err := ch.QueueDeclare(broker.EventFlashSaleOrder, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("flash sale consumer: declare queue: %w", err))
	}
	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("flash sale consumer: consume: %w", err))
	}
	logrus.Infof("Flash sale consumer started on queue: %s x %d workers",
		broker.EventFlashSaleOrder, flashSaleOrderWorkers)

	// Bounded worker pool: 启动固定数量的 goroutine 从同一个 channel 抢消息。
	// 不要用 `for msg := range msgs { go c.handle... }` 模式 —
	// 那样每条消息起一个新 goroutine,高并发下无界 goroutine 爆炸 +
	// 下游 (MySQL / Mongo / gRPC) 被打爆失控。
	for i := 0; i < flashSaleOrderWorkers; i++ {
		go func(workerID int) {
			for msg := range msgs {
				c.handleFlashSaleOrder(ch, msg, q)
			}
		}(i)
	}
}

func (c *Consumer) handleFlashSaleOrder(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(
		broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers),
		fmt.Sprintf("rabbitmq.%s.consume", q.Name),
	)
	defer span.End()

	start := time.Now()
	var err error
	var createErr error
	defer func() {
		// Business outcome (success | insufficient_stock | error).
		// err covers pre-business failures (unmarshal); createErr covers business.
		result := "error"
		if err == nil {
			result = metrics.ClassifyFlashOrderError(createErr)
		}
		duration := time.Since(start).Seconds()
		metrics.FlashOrderTotal.WithLabelValues(result).Inc()
		metrics.FlashOrderDuration.WithLabelValues(result).Observe(duration)

		// Consumer-level health (ack | nack).
		consumerResult := "ack"
		if err != nil || createErr != nil {
			consumerResult = "nack"
		}
		metrics.RabbitConsumeTotal.WithLabelValues(q.Name, consumerResult).Inc()
		metrics.RabbitConsumeDuration.WithLabelValues(q.Name).Observe(duration)

		if err != nil {
			logging.Warnf(ctx, nil, "flash sale consume failed: %v", err)
			_ = msg.Nack(false, false)
		} else {
			_ = msg.Ack(false)
		}
	}()

	var payload broker.FlashSaleOrderPayload
	if err = json.Unmarshal(msg.Body, &payload); err != nil {
		return
	}

	result, createErr := c.app.Commands.CreateFlashOrder.Handle(ctx, command.CreateFlashOrder{
		CustomerID: payload.CustomerID,
		Items:      toItemWithQuantities(payload.Items),
	})

	resultKey := flashResultKeyPrefix + payload.Token

	if createErr != nil {
		c.compensateRedis(ctx, &payload)
		b, _ := json.Marshal(flashResultPayload{Status: "failed", Message: createErr.Error()})
		_ = c.redisClient.Set(ctx, resultKey, string(b), flashResultTTL)
		return
	}

	b, _ := json.Marshal(flashResultPayload{Status: "success", OrderID: result.OrderID})
	_ = c.redisClient.Set(ctx, resultKey, string(b), flashResultTTL)
}

// compensateRedis 在 CreateFlashOrder saga 失败时把 Redis 入口闸的扣减撤回。
// 统一调 redis.FlashSaleRollback(Lua 原子) — 和 HTTP 路径走同一段代码。
//
// Key 构造按 ADR-0004:
//   - 新消息 (ActivityID 非空):flash:stock:activity_{id}:{product_id} 带 hash tag
//   - 旧消息 (ActivityID 空):退化到无 activity 前缀的旧 key 格式,向后兼容
func (c *Consumer) compensateRedis(ctx context.Context, payload *broker.FlashSaleOrderPayload) {
	for _, item := range payload.Items {
		var stockKey, onceKey string
		if payload.ActivityID != "" {
			stockKey = fmt.Sprintf("flash:stock:activity_%s:{%s}", payload.ActivityID, item.ItemID)
			onceKey = fmt.Sprintf("flash:once:activity_%s:%s:{%s}", payload.ActivityID, payload.CustomerID, item.ItemID)
		} else {
			stockKey = flashStockKeyPrefix + item.ItemID
			onceKey = fmt.Sprintf("%s%s:%s", flashOnceKeyPrefix, payload.CustomerID, item.ItemID)
		}
		result, err := redis.FlashSaleRollback(ctx, c.redisClient, stockKey, onceKey, int64(item.Quantity))
		if err != nil {
			logging.Errorf(ctx, nil, "flash rollback exhausted retries stock=%s once=%s: %v", stockKey, onceKey, err)
			metrics.FlashRollbackTotal.WithLabelValues("error").Inc()
			continue
		}
		if result == redis.FlashRollbackSkipped {
			logging.Warnf(ctx, nil, "flash rollback skipped (stock key expired) for %s", stockKey)
			metrics.FlashRollbackTotal.WithLabelValues("skipped").Inc()
		} else {
			metrics.FlashRollbackTotal.WithLabelValues("applied").Inc()
		}
	}
}

func toItemWithQuantities(items []broker.FlashSaleItem) []*entity.ItemWithQuantity {
	out := make([]*entity.ItemWithQuantity, 0, len(items))
	for _, i := range items {
		out = append(out, &entity.ItemWithQuantity{ID: i.ItemID, Quantity: i.Quantity})
	}
	return out
}
