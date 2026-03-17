package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/ecstasoy/gorder/common/config"
	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	"github.com/ecstasoy/gorder/payment/domain"
	"github.com/gin-gonic/gin"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
	"github.com/stripe/stripe-go/v80"
	"github.com/stripe/stripe-go/v80/webhook"
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
	logrus.Info("Got webhook request from Stripe")
	const MaxBodyBytes = int64(65536)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes)
	payload, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		logrus.Infof("Error reading body: %v", err)
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
		logrus.Infof("Error parsing webhook event: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Error parsing webhook event"})
		return
	}

	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		var session stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
			logrus.Infof("Error parsing webhook event data: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Error parsing webhook event data"})
			return
		}

		if session.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
			logrus.Info("Payment successful for session: ", session.ID)
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			var items []*orderpb.Item
			_ = json.Unmarshal([]byte(session.Metadata["items"]), &items)
			marshalledOrder, err := json.Marshal(&domain.Order{
				ID:          session.Metadata["orderID"],
				CustomerID:  session.Metadata["customerID"],
				Status:      orderpb.OrderStatus_ORDER_STATUS_PAID,
				PaymentLink: session.Metadata["paymentLink"],
				Items:       items,
			})
			if err != nil {
				logrus.Infof("Error marshalling order: %v", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Error processing order data"})
				return
			}
			h.channel.PublishWithContext(ctx, broker.EventOrderPaid, "", false, false, amqp.Publishing{
				ContentType:  "application/json",
				Body:         marshalledOrder,
				DeliveryMode: amqp.Persistent,
			})
			logrus.Infof("message published to queue %s for orderID: %s", broker.EventOrderPaid, session.Metadata["orderID"])
		}
	default:
		fmt.Fprintf(os.Stderr, "Unhandled event type: %s\n", event.Type)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Webhook received successfully"})
}
