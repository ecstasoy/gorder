package decorator

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

type queryLoggingDecorator[Q any, R any] struct {
	logger *logrus.Entry
	base   QueryHandler[Q, R]
}

func (q queryLoggingDecorator[Q, R]) Handle(ctx context.Context, query Q) (result R, err error) {
	logger := q.logger.WithFields(logrus.Fields{
		"query":       query,
		"action_name": generateActionName(query),
		"query_body":  fmt.Sprintf("%+v", query),
	})
	logger.Debug("Executing query")
	defer func() {
		if err == nil {
			logger.Info("query executed successfully")
		} else {
			logger.WithError(err).Error("query executed failed")
		}
	}()
	return q.base.Handle(ctx, query)
}

func generateActionName(query any) string {
	return strings.Split(fmt.Sprintf("%T", query), ".")[1]
}
