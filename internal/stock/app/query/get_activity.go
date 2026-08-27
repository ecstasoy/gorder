package query

import (
	"context"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/stock/domain/activity"
	"github.com/sirupsen/logrus"
)

// GetActivity 给 order 侧查活动元数据(显示活动名 / 时段 / 是否 live)。
type GetActivity struct {
	ActivityID string
}

type GetActivityHandler decorator.QueryHandler[GetActivity, *activity.Activity]

type getActivityHandler struct {
	repo activity.Repository
}

func NewGetActivityHandler(
	repo activity.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) GetActivityHandler {
	if repo == nil {
		panic("nil activity repo")
	}
	return decorator.ApplyQueryDecorators(
		getActivityHandler{repo: repo},
		logger,
		metricsClient,
	)
}

func (h getActivityHandler) Handle(ctx context.Context, q GetActivity) (*activity.Activity, error) {
	return h.repo.Get(ctx, q.ActivityID)
}
