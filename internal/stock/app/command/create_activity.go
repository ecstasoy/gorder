package command

import (
	"context"
	"time"

	"github.com/ecstasoy/gorder/common/decorator"
	"github.com/ecstasoy/gorder/stock/domain/activity"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// CreateActivity 创建一场 flash sale 活动 (draft 状态)。
// 不立即 warmup —— 由运维或 scheduler 在合适时机调 WarmUpActivity。
type CreateActivity struct {
	Name       string
	ProductID  string
	TotalStock int32
	StartTime  time.Time
	EndTime    time.Time
}

type CreateActivityResult struct {
	ActivityID string
}

type CreateActivityHandler decorator.CommandHandler[CreateActivity, *CreateActivityResult]

type createActivityHandler struct {
	repo activity.Repository
}

func NewCreateActivityHandler(
	repo activity.Repository,
	logger *logrus.Logger,
	metricsClient decorator.MetricsClient,
) CreateActivityHandler {
	if repo == nil {
		panic("nil activity repo")
	}
	return decorator.ApplyCommandDecorators(
		createActivityHandler{repo: repo},
		logger,
		metricsClient,
	)
}

func (h createActivityHandler) Handle(ctx context.Context, cmd CreateActivity) (*CreateActivityResult, error) {
	id := uuid.NewString()
	a, err := activity.NewActivity(id, cmd.Name, cmd.ProductID, cmd.TotalStock, cmd.StartTime, cmd.EndTime)
	if err != nil {
		return nil, errors.Wrap(err, "CreateActivity: build aggregate")
	}
	if err := h.repo.Create(ctx, a); err != nil {
		return nil, err
	}
	return &CreateActivityResult{ActivityID: id}, nil
}
