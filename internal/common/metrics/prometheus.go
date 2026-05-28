package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	commandDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gorder_command_duration_seconds",
			Help:    "CQRS command handler duration in seconds.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"handler", "status"},
	)
	commandTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gorder_command_total",
			Help: "Total number of CQRS commands handled, partitioned by handler and status.",
		},
		[]string{"handler", "status"},
	)
	queryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gorder_query_duration_seconds",
			Help:    "CQRS query handler duration in seconds.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"handler", "status"},
	)
	queryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gorder_query_total",
			Help: "Total number of CQRS queries handled, partitioned by handler and status.",
		},
		[]string{"handler", "status"},
	)
)

type PrometheusMetricsClient struct{}

func NewPrometheusMetricsClient() PrometheusMetricsClient {
	return PrometheusMetricsClient{}
}

func (PrometheusMetricsClient) IncCommand(handler string, success bool, d time.Duration) {
	status := statusLabel(success)
	commandDuration.WithLabelValues(handler, status).Observe(d.Seconds())
	commandTotal.WithLabelValues(handler, status).Inc()
}

func (PrometheusMetricsClient) IncQuery(handler string, success bool, d time.Duration) {
	status := statusLabel(success)
	queryDuration.WithLabelValues(handler, status).Observe(d.Seconds())
	queryTotal.WithLabelValues(handler, status).Inc()
}

func statusLabel(success bool) string {
	if success {
		return "success"
	}

	return "failure"
}
