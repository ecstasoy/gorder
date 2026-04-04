package decorator

import (
	"context"
	"fmt"
	"strings"
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
	actionName := strings.ToLower(generateActionName(query))
	defer func() {
		end := time.Since(start)
		q.client.Inc(fmt.Sprintf("query.%s.duration", actionName), int(end.Seconds()))
		if err == nil {
			q.client.Inc(fmt.Sprintf("query.%s.success", actionName), 1)
		} else {
			q.client.Inc(fmt.Sprintf("query.%s.failure", actionName), 1)
		}
	}()
	return q.base.Handle(ctx, query)
}

type commandMetricsDecorator[C, R any] struct {
	base   CommandHandler[C, R]
	client MetricsClient
}

func (c commandMetricsDecorator[C, R]) Handle(ctx context.Context, cmd C) (result R, err error) {
	start := time.Now()
	actionName := strings.ToLower(generateActionName(cmd))
	defer func() {
		end := time.Since(start)
		c.client.Inc(fmt.Sprintf("command.%s.duration", actionName), int(end.Seconds()))
		if err == nil {
			c.client.Inc(fmt.Sprintf("command.%s.success", actionName), 1)
		} else {
			c.client.Inc(fmt.Sprintf("command.%s.failure", actionName), 1)
		}
	}()
	return c.base.Handle(ctx, cmd)
}
