package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/convertor"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/pkg/errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

type Consumer struct {
	orderGRPC OrderService
}

type OrderService interface {
	UpdateOrder(ctx context.Context, request *orderpb.Order) error
}

func NewConsumer(orderGRPC OrderService) *Consumer {
	return &Consumer{
		orderGRPC: orderGRPC,
	}
}

const (
	kitchenPrefetch = 50
	kitchenWorkers  = 20
)

func (c *Consumer) Listen(ch *amqp.Channel) {
	// Prefetch + worker pool, cook() 本身 sleep 5 秒模拟备餐,
	// 串行的话吞吐上限只有 0.2 orders/sec,并行后能到 ~4 orders/sec
	if err := ch.Qos(kitchenPrefetch, 0, false); err != nil {
		logrus.Fatal(fmt.Errorf("failed to set QoS: %w", err))
	}

	q, err := ch.QueueDeclare("", true, false, true, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare queue: %w", err))
	}

	if err = ch.QueueBind(q.Name, "", broker.EventOrderPaid, false, nil); err != nil {
		logrus.Fatal(fmt.Errorf("failed to bind queue: %w", err))
	}

	msgs, err := ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		// 之前这里只是 Warnf 还用错了 %w 格式符; Consume 失败应该直接 Fatal,
		// 否则 goroutine 会 block 在 nil channel 上变僵尸
		logrus.Fatalf("failed to consume message: %v", err)
	}

	logrus.Infof("Kitchen consumer started on anonymous queue: %s x %d workers", q.Name, kitchenWorkers)

	for i := 0; i < kitchenWorkers; i++ {
		go func(workerID int) {
			for msg := range msgs {
				c.handleMessage(ch, msg, q)
			}
		}(i)
	}

	var forever chan struct{}
	<-forever
}

func (c *Consumer) handleMessage(ch *amqp.Channel, msg amqp.Delivery, q amqp.Queue) {
	tr := otel.Tracer("rabbitmq")
	ctx, span := tr.Start(broker.ExtractRabbitMQHeaders(context.Background(), msg.Headers), fmt.Sprintf("rabbitmq.%s.consume", q.Name))
	var err error
	defer func() {
		span.End()
		if err != nil {
			logging.Warnf(ctx, nil, "Failed to consume message: %v, error: %v", string(msg.Body), err)
			_ = msg.Nack(false, false)
		} else {
			_ = msg.Ack(false)
			logging.Infof(ctx, nil, "Message consumed successfully: %s", string(msg.Body))
		}
	}()

	o := &entity.Order{}
	if err := json.Unmarshal(msg.Body, o); err != nil {
		err = errors.Wrap(err, "failed to unmarshal message body")
		return
	}

	logrus.WithContext(ctx).Infof("Kitchen received order: ID=%s, Status=%s", o.ID, o.Status.String())

	if o.Status != orderpb.OrderStatus_ORDER_STATUS_PAID {
		logrus.Warnf("Order status is not PAID: %s", o.Status.String())
		err = errors.Errorf("order not paid (status=%s), cannot cook", o.Status.String())
		return
	}

	cook(ctx, o)
	span.AddEvent(fmt.Sprintf("order_cook: %v", o))
	if err = c.orderGRPC.UpdateOrder(ctx, &orderpb.Order{
		ID:          o.ID,
		CustomerID:  o.CustomerID,
		Status:      orderpb.OrderStatus_ORDER_STATUS_PREPARING,
		Items:       convertor.NewItemConvertor().EntitiesToProtos(o.Items),
		PaymentLink: o.PaymentLink,
	}); err != nil {
		logging.Errorf(ctx, nil, "Failed to update order: %v", err)
		if retryErr := broker.HandleRetry(ctx, ch, &msg); retryErr != nil {
			logging.Errorf(ctx, nil, "Failed to handle retry for orderID %s: %v", o.ID, retryErr)
		}
		return
	}
	span.AddEvent("kitchen.order.finished.updated")
}

// cook simulates the cooking process by sleeping for a few seconds.
// In a real application, this would involve more complex logic and interactions with other services or databases.
func cook(ctx context.Context, o *entity.Order) {
	logrus.WithContext(ctx).Printf("cooking order: %s", o.ID)
	time.Sleep(5 * time.Second)
	logrus.WithContext(ctx).Printf("order %s done!", o.ID)
}
