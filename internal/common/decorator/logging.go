package decorator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ecstasoy/gorder/common/logging"
	"github.com/sirupsen/logrus"
)

type queryLoggingDecorator[Q any, R any] struct {
	logger *logrus.Logger
	base   QueryHandler[Q, R]
}

func (q queryLoggingDecorator[Q, R]) Handle(ctx context.Context, query Q) (result R, err error) {
	body, _ := json.Marshal(query)
	fields := logrus.Fields{
		"query":      generateActionName(query),
		"query_body": string(body),
	}
	defer func() {
		if err == nil {
			logging.Infof(ctx, fields, "%s", "Query execute successfully")
		} else {
			logging.Errorf(ctx, fields, "Failed to execute query, err=%v", err)
		}
	}()
	result, err = q.base.Handle(ctx, query)
	return result, err
}

type commandLoggingDecorator[C, R any] struct {
	logger *logrus.Logger
	base   CommandHandler[C, R]
}

func (q commandLoggingDecorator[C, R]) Handle(ctx context.Context, cmd C) (result R, err error) {
	body, _ := json.Marshal(cmd)
	fields := logrus.Fields{
		"command":      generateActionName(cmd),
		"command_body": string(body),
	}
	defer func() {
		if err == nil {
			logging.Infof(ctx, fields, "%s", "Query execute successfully")
		} else {
			logging.Errorf(ctx, fields, "Failed to execute query, err=%v", err)
		}
	}()
	result, err = q.base.Handle(ctx, cmd)
	return result, err
}

func generateActionName(query any) string {
	return strings.Split(fmt.Sprintf("%T", query), ".")[1]
}
