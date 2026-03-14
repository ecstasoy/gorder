package decorator

import (
	"context"
	"fmt"
	"time"
)

type MetricsClient interface {
	Inc(key string, value int)
}

type queryMetricsDecorator[Q any, R any] struct {
	base   QueryHandler[Q, R]
	client MetricsClient
}

func (q queryMetricsDecorator[Q, R]) Handle(ctx context.Context, query Q) (result R, err error) {
	start := time.Now()

	defer func() {
		end := time.Since(start)
		q.client.Inc(fmt.Sprintf("query.%s.duration", generateActionName(query)), int(end.Seconds()))
		if err == nil {
			q.client.Inc(fmt.Sprintf("query.%s.success", generateActionName(query)), 1)
		} else {
			q.client.Inc(fmt.Sprintf("query.%s.failure", generateActionName(query)), 1)
		}
	}()
	return q.base.Handle(ctx, query)
}
