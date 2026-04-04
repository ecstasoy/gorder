package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/handler/redis"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/ecstasoy/gorder/order/app/command"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

const flashResultTTL = 24 * time.Hour

type flashResultPayload struct {
	Status  string `json:"status"`
	OrderID string `json:"order_id,omitempty"`
	Message string `json:"message,omitempty"`
}

func (c *Consumer) ListenFlashSaleOrders(ch *amqp.Channel) {
	q, err := ch.QueueDeclare(broker.EventFlashSaleOrder, true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("flash sale consumer: declare queue: %w", err))
	}
	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("flash sale consumer: consume: %w", err))
	}
	logrus.Infof("Flash sale consumer started on queue: %s", broker.EventFlashSaleOrder)
	for msg := range msgs {
		c.handleFlashSaleOrder(ch, msg, q)
	}
}

func (c *Consumer) handleFlashSaleOrder(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	t := otel.Tracer("rabbitmq")
	ctx, span := t.Start(
		broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers),
		fmt.Sprintf("rabbitmq.%s.consume", q.Name),
	)
	defer span.End()

	var err error
	defer func() {
		if err != nil {
			logging.Warnf(ctx, nil, "flash sale consume failed: %v", err)
			_ = msg.Nack(false, false)
		} else {
			_ = msg.Ack(false)
		}
	}()

	// 1. 解析消息
	var payload broker.FlashSaleOrderPayload
	if err = json.Unmarshal(msg.Body, &payload); err != nil {
		return
	}

	// 2. 转成 proto items → entity items
	protoItems := make([]*orderpb.ItemWithQuantity, 0, len(payload.Items))
	for _, item := range payload.Items {
		protoItems = append(protoItems, &orderpb.ItemWithQuantity{
			ItemID: item.ItemID, Quantity: item.Quantity,
		})
	}

	// 3. 调用 CreateOrder
	result, createErr := c.app.Commands.CreateOrder.Handle(ctx, command.CreateOrder{
		CustomerID: payload.CustomerID,
		Items:      convertor.NewItemWithQuantityConvertor().ProtosToEntities(protoItems),
	})

	// 4. 把结果写入 Redis
	redisClient := redis.LocalClient()
	resultKey := "flash:result:" + payload.Token
	if createErr != nil {
		b, _ := json.Marshal(flashResultPayload{Status: "failed", Message: createErr.Error()})
		_ = redisClient.Set(ctx, resultKey, string(b), flashResultTTL)
		return
	}
	b, _ := json.Marshal(flashResultPayload{Status: "success", OrderID: result.OrderID})
	_ = redisClient.Set(ctx, resultKey, string(b), flashResultTTL)
}
