package broker

import (
	"context"
	"encoding/json"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/pkg/errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
)

const (
	EventOrderCreated        = "order.created"
	EventOrderPaid           = "order.paid"
	EventOrderPaymentTimeout = "order.payment.timeout"
	OrderPaymentDelayQueue   = "order.payment.delay"
	OrderPaymentTimeoutDLX   = "order.payment.timeout.dlx"
	EventOrderRefund         = "order.refund"
	EventFlashSaleOrder      = "flash.order.created"
)

// OrderPaidEvent 在原有 Order 字段基础上携带 PaymentIntentID，
// 用于在状态冲突时发起退款。JSON 字段名与 entity.Order 保持一致，
// 不破坏 kitchen/order consumer 的现有反序列化。
type OrderPaidEvent struct {
	ID              string              `json:"ID"`
	CustomerID      string              `json:"CustomerID"`
	Status          orderpb.OrderStatus `json:"Status"`
	PaymentLink     string              `json:"PaymentLink"`
	Items           []*entity.Item      `json:"Items"`
	PaymentIntentID string              `json:"PaymentIntentID,omitempty"`
}

// OrderRefundPayload 是退款事件的消息体。
type OrderRefundPayload struct {
	OrderID         string `json:"OrderID"`
	CustomerID      string `json:"CustomerID"`
	PaymentIntentID string `json:"PaymentIntentID"`
}

type FlashSaleOrderPayload struct {
	Token      string          `json:"token"`
	CustomerID string          `json:"customer_id"`
	Items      []FlashSaleItem `json:"items"`
}
type FlashSaleItem struct {
	ItemID   string `json:"item_id"`
	Quantity int32  `json:"quantity"`
}

type RoutingType string

const (
	FanOut RoutingType = "fan-out"
	Direct RoutingType = "direct"
)

type PublishEventReq struct {
	Channel  *amqp.Channel
	Routing  RoutingType
	Queue    string
	Exchange string
	Body     any
}

func PublishEvent(ctx context.Context, p PublishEventReq) (err error) {
	_, dLog := logging.WhenEventPublish(ctx, p)
	defer dLog(nil, &err)

	if err = checkParam(p); err != nil {
		return err
	}

	switch p.Routing {
	default:
		logrus.WithContext(ctx).Panicf("unsupported routing type: %s", string(p.Routing))
	case FanOut:
		return fanOut(ctx, p)
	case Direct:
		return directQueue(ctx, p)
	}
	return nil
}

func checkParam(p PublishEventReq) error {
	if p.Channel == nil {
		return errors.New("nil channel")
	}
	return nil
}

func directQueue(ctx context.Context, p PublishEventReq) (err error) {
	_, err = p.Channel.QueueDeclare(p.Queue, true, false, false, false, nil)
	if err != nil {
		return err
	}
	jsonBody, err := json.Marshal(p.Body)
	if err != nil {
		return err
	}
	return doPublish(ctx, p.Channel, p.Exchange, p.Queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         jsonBody,
		Headers:      InjectRabbitMQHeaders(ctx),
	})
}

func PublishToDelayQueue(ctx context.Context, ch *amqp.Channel, body any) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return doPublish(ctx, ch, "", OrderPaymentDelayQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         jsonBody,
		Headers:      InjectRabbitMQHeaders(ctx),
	})
}

func fanOut(ctx context.Context, p PublishEventReq) (err error) {
	jsonBody, err := json.Marshal(p.Body)
	if err != nil {
		return err
	}
	return doPublish(ctx, p.Channel, p.Exchange, "", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         jsonBody,
		Headers:      InjectRabbitMQHeaders(ctx),
	})
}

func doPublish(ctx context.Context, ch *amqp.Channel, exchange, key string, mandatory bool, immediate bool, msg amqp.Publishing) error {
	if err := ch.PublishWithContext(ctx, exchange, key, mandatory, immediate, msg); err != nil {
		logging.Warnf(ctx, nil, "_publish_event_failed || exchange=%s || key=%s || msg=%v", exchange, key, msg)
		return errors.Wrap(err, "publish event error")
	}
	return nil
}
