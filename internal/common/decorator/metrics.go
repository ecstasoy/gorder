package decorator

import (
	"context"
	"strings"
	"time"
)

type MetricsClient interface {
	IncCommand(handler string, success bool, duration time.Duration)
	IncQuery(handler string, success bool, duration time.Duration)
}

type queryMetricsDecorator[Q any, R any] struct {
	base   QueryHandler[Q, R]
	client MetricsClient
}

func (q queryMetricsDecorator[Q, R]) Handle(ctx context.Context, query Q) (result R, err error) {
	start := time.Now()
	actionName := strings.ToLower(generateActionName(query))
	defer func() {
		q.client.IncQuery(actionName, err == nil, time.Since(start))
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
		c.client.IncCommand(actionName, err == nil, time.Since(start))
	}()
	return c.base.Handle(ctx, cmd)
}
