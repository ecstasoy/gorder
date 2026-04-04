package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/common/logging"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"github.com/stripe/stripe-go/v80"
	"github.com/stripe/stripe-go/v80/webhook"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type PaymentHandler struct {
	channel *amqp.Channel
}

func NewPaymentHandler(ch *amqp.Channel) *PaymentHandler {
	return &PaymentHandler{channel: ch}
}

func (h *PaymentHandler) RegisterRoutes(c *gin.Engine) {
	c.POST("api/webhook", h.HandleWebHook)
}

func (h *PaymentHandler) HandleWebHook(c *gin.Context) {
	logrus.WithContext(c.Request.Context()).Info("Got webhook request from Stripe")
	var err error
	defer func() {
		if err != nil {
			logging.Warnf(c.Request.Context(), nil, "Error handling webhook: %v", err)
		} else {
			logging.Infof(c.Request.Context(), nil, "Webhook handled successfully")
		}
	}()

	const MaxBodyBytes = int64(65536)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes)
	payload, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		err = errors.Wrap(err, "error reading body")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Error reading request body"})
		return
	}

	event, err := webhook.ConstructEventWithOptions(
		payload,
		c.Request.Header.Get("Stripe-Signature"),
		config.GetStringWithEnv("endpoint-stripe-secret"),
		webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true,
		},
	)
	if err != nil {
		err = errors.Wrap(err, "error verifying webhook signature")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Error parsing webhook event"})
		return
	}

	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		var session stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
			err = errors.Wrap(err, "error unmarshalling checkout session data")
			c.JSON(http.StatusBadRequest, err.Error())
			return
		}

		if session.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
			var items []*entity.Item
			_ = json.Unmarshal([]byte(session.Metadata["items"]), &items)

			t := otel.Tracer("rabbitmq")
			ctx, span := t.Start(c.Request.Context(), fmt.Sprintf("rabbitmq.%s.publish", broker.EventOrderPaid))
			defer span.End()

			_, publishSpan := otel.Tracer("rabbitmq").Start(ctx, "rabbitmq.order.paid.publish")
			defer publishSpan.End()

			paymentIntentID := ""
			if session.PaymentIntent != nil {
				paymentIntentID = session.PaymentIntent.ID
			}

			publishSpan.SetAttributes(
				attribute.String("order.id", session.Metadata["orderID"]),
				attribute.String("customer.id", session.Metadata["customerID"]),
				attribute.String("exchange", broker.EventOrderPaid),
			)

			_ = broker.PublishEvent(ctx, broker.PublishEventReq{
				Channel:  h.channel,
				Routing:  broker.FanOut,
				Queue:    "",
				Exchange: broker.EventOrderPaid,
				Body: broker.OrderPaidEvent{
					ID:              session.Metadata["orderID"],
					CustomerID:      session.Metadata["customerID"],
					Status:          orderpb.OrderStatus_ORDER_STATUS_PAID,
					PaymentLink:     session.Metadata["paymentLink"],
					Items:           items,
					PaymentIntentID: paymentIntentID,
				},
			})
			publishSpan.AddEvent("message.published")
		}
	default:
		logrus.WithContext(c.Request.Context()).Infof("Unhandled event type: %s", event.Type)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Webhook received successfully"})
}
