package metrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Flash sale outcomes — the headline metric for load-test storytelling.
	// result = success | insufficient_stock | error
	FlashOrderTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gorder_flash_order_total",
			Help: "Flash sale order outcomes from the order service perspective.",
		},
		[]string{"result"},
	)
	FlashOrderDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gorder_flash_order_duration_seconds",
			Help:    "End-to-end flash sale order handling from message receive to ack/nack.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"result"},
	)

	// Low-level MySQL stock deduction.
	// result = success | insufficient | error
	StockDeductTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gorder_stock_deduct_total",
			Help: "Outcomes of MySQL-backed stock deduction attempts (CAS layer).",
		},
		[]string{"result"},
	)

	// Compensation fires — should be near zero in a healthy system.
	// reason = persist_failed | publish_failed | other
	StockRestoreTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gorder_stock_restore_total",
			Help: "Stock restoration (compensating transaction) invocations. Healthy system should stay near zero.",
		},
		[]string{"reason"},
	)

	// RabbitMQ consumer health — generic, keyed by queue.
	// result = ack | nack
	RabbitConsumeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gorder_rabbitmq_consume_total",
			Help: "RabbitMQ message consumption outcomes.",
		},
		[]string{"queue", "result"},
	)
	RabbitConsumeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gorder_rabbitmq_consume_duration_seconds",
			Help:    "RabbitMQ message handling duration, from receive to ack/nack.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"queue"},
	)

	// Flash reserve — the HTTP hot path. Almost all flash-sale rejects land
	// here (Redis Lua rejects). Reserve == "ok" means the message was enqueued
	// and will later be visible as gorder_flash_order_total at the consumer.
	// result = ok | insufficient | duplicate | not_active | error
	FlashReserveTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gorder_flash_reserve_total",
			Help: "Outcomes of the flash sale HTTP reserve step (Redis Lua + enqueue).",
		},
		[]string{"result"},
	)
	FlashReserveDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gorder_flash_reserve_duration_seconds",
			Help:    "Latency of the flash sale HTTP reserve handler end-to-end.",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
	)
)

// ClassifyFlashOrderError maps a business error to a Prometheus label value.
// String-matching is fragile but acceptable for metrics — if misclassification
// is 1% it's still useful. Upgrade to typed sentinel errors when convenient.
func ClassifyFlashOrderError(err error) string {
	if err == nil {
		return "success"
	}
	s := err.Error()
	if strings.Contains(s, "insufficient stock") || strings.Contains(s, "insufficient_stock") {
		return "insufficient_stock"
	}
	return "error"
}
