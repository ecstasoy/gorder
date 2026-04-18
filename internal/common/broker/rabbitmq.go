package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/logging"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
)

const (
	DLX                = "dlx"
	DLQ                = "dlq"
	amqpRetryHeaderKey = "x-retry-count"
)

var (
	maxRetryCount = viper.GetInt64("rabbitmq.max-retry-count")
)

// Connect 建立到 RabbitMQ 的连接。返回:
//   - *amqp.Connection: 长连接,consumer 可以用它 conn.Channel() 各自开独立 channel
//     (channel 在 AMQP 协议层不是 goroutine-safe,不同的 consumer / publisher 必须
//     用不同的 channel,否则会出现 503 "unexpected command received" 错误)
//   - *amqp.Channel: 专用于 publisher 的共享 channel,配合 publishMutex 串行化 publish
//   - func() error: 清理函数,会关闭 channel 和 connection
func Connect(user, pwd, host, port string) (*amqp.Connection, *amqp.Channel, func() error) {
	address := fmt.Sprintf("amqp://%s:%s@%s:%s/", user, pwd, host, port)
	logrus.Infof("Connecting to RabbitMQ at %s", address)

	conn, err := amqp.Dial(address)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to connect to rabbitmq: %w", err))
	}

	ch, err := conn.Channel()
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to open channel: %w", err))
	}

	err = ch.ExchangeDeclare(EventOrderCreated, "direct", true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare %s exchange: %w", EventOrderCreated, err))
	}

	err = ch.ExchangeDeclare(EventOrderPaid, "fanout", true, false, false, false, nil)
	if err != nil {
		logrus.Fatal(fmt.Errorf("failed to declare %s exchange: %w", EventOrderPaid, err))
	}

	if err := createDLX(ch); err != nil {
		logrus.Fatal(fmt.Errorf("failed to create DLX: %w", err))
	}

	if err := createPaymentTimeoutQueue(ch); err != nil {
		logrus.Fatal(fmt.Errorf("failed to create payment timeout queue: %w", err))
	}

	// 启动时一次性声明所有 direct queue,避免 PublishEvent 路径上做并发 QueueDeclare
	// (QueueDeclare 和 Publish 一样是 channel 级操作,不是 thread-safe)
	for _, q := range []string{
		EventOrderCreated,
		EventFlashSaleOrder,
		EventOrderRefund,
	} {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			logrus.Fatal(fmt.Errorf("failed to declare queue %s: %w", q, err))
		}
	}

	logrus.Info("Successfully connected to RabbitMQ")
	return conn, ch, func() error {
		_ = ch.Close()
		return conn.Close()
	}
}

func createPaymentTimeoutQueue(ch *amqp.Channel) error {
	// 1. 声明死信交换机（direct 类型）
	if err := ch.ExchangeDeclare(
		OrderPaymentTimeoutDLX, "direct",
		true, false, false, false, nil,
	); err != nil {
		return err
	}

	// 2. 声明最终消费队列（timeout 队列）
	if _, err := ch.QueueDeclare(
		EventOrderPaymentTimeout,
		true, false, false, false, nil,
	); err != nil {
		return err
	}

	// 3. 绑定 timeout 队列到死信交换机
	if err := ch.QueueBind(
		EventOrderPaymentTimeout, EventOrderPaymentTimeout,
		OrderPaymentTimeoutDLX, false, nil,
	); err != nil {
		return err
	}

	// 4. 声明延迟队列（带 TTL 和 DLX 参数）
	ttl := viper.GetInt("rabbitmq.payment-timeout-ms") // 例如 900000 = 15分钟
	if ttl == 0 {
		ttl = 900000
	}
	_, err := ch.QueueDeclare(
		OrderPaymentDelayQueue,
		true, false, false, false,
		amqp.Table{
			"x-message-ttl":             int32(ttl),
			"x-dead-letter-exchange":    OrderPaymentTimeoutDLX,
			"x-dead-letter-routing-key": EventOrderPaymentTimeout,
		},
	)
	return err
}

func createDLX(ch *amqp.Channel) interface{} {
	q, err := ch.QueueDeclare("share_queue", true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("failed to declare order.mq queue: %w", err)
	}

	err = ch.ExchangeDeclare(DLX, "fanout", true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("failed to declare DLX exchange: %w", err)
	}
	err = ch.QueueBind(q.Name, "", DLX, false, nil)
	if err != nil {
		return fmt.Errorf("failed to bind order.mq queue to DLX: %w", err)
	}

	_, err = ch.QueueDeclare(DLQ, true, false, false, false, nil)

	return err
}

func HandleRetry(ctx context.Context, ch *amqp.Channel, d *amqp.Delivery) (err error) {
	fields, dLog := logging.WhenRequest(ctx, "HandleRetry", map[string]any{
		"delivery":        d,
		"max_retry_count": maxRetryCount,
	})
	defer dLog(nil, &err)

	if d.Headers == nil {
		d.Headers = amqp.Table{}
	}
	retryCount, ok := d.Headers[amqpRetryHeaderKey].(int64)
	if !ok {
		retryCount = 0
	}
	retryCount++
	d.Headers[amqpRetryHeaderKey] = retryCount
	fields["retry_count"] = retryCount

	if retryCount >= maxRetryCount {
		logrus.WithContext(ctx).Infof("Publishing message %s to dlq", d.MessageId)
		return doPublish(ctx, ch, "", DLQ, false, false, amqp.Publishing{
			Headers:      d.Headers,
			ContentType:  "application/json",
			Body:         d.Body,
			DeliveryMode: amqp.Persistent,
		})
	}
	logrus.WithContext(ctx).Debugf("Retrying publishing message %s, count=%d", d.MessageId, retryCount)
	time.Sleep(time.Second * time.Duration(retryCount))
	return doPublish(ctx, ch, d.Exchange, d.RoutingKey, false, false, amqp.Publishing{
		Headers:      d.Headers,
		ContentType:  "application/json",
		Body:         d.Body,
		DeliveryMode: amqp.Persistent,
	})
}

type RabbitMQHeaderCarrier map[string]interface{}

func (r RabbitMQHeaderCarrier) Get(key string) string {
	value, ok := r[key]
	if !ok {
		return ""
	}
	return value.(string)
}

func (r RabbitMQHeaderCarrier) Set(key, value string) {
	r[key] = value
}

func (r RabbitMQHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	return keys
}

func InjectRabbitMQHeaders(ctx context.Context) map[string]interface{} {
	carrier := make(RabbitMQHeaderCarrier)
	otel.GetTextMapPropagator().Inject(ctx, &carrier)
	return carrier
}

func ExtractRabbitMQHeaders(ctx context.Context, headers map[string]interface{}) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, RabbitMQHeaderCarrier(headers))
}
