package adapters

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/ecstasoy/gorder/stock/domain/activity"
	"github.com/ecstasoy/gorder/stock/infra/persistent"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

// FlashActivityModel 映射 flash_activities 表(见 init.sql)。
type FlashActivityModel struct {
	ID         string    `gorm:"primaryKey;column:id"`
	Name       string    `gorm:"column:name"`
	ProductID  string    `gorm:"column:product_id"`
	TotalStock int32     `gorm:"column:total_stock"`
	StartTime  time.Time `gorm:"column:start_time"`
	EndTime    time.Time `gorm:"column:end_time"`
	Status     string    `gorm:"column:status"`
	WarmupDone bool      `gorm:"column:warmup_done"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (FlashActivityModel) TableName() string { return "flash_activities" }

type MySQLActivityRepository struct {
	db *persistent.MySQL
}

func NewMySQLActivityRepository(db *persistent.MySQL) *MySQLActivityRepository {
	return &MySQLActivityRepository{db: db}
}

func (r *MySQLActivityRepository) Create(ctx context.Context, a *activity.Activity) error {
	m := toModel(a)
	if err := r.db.GetDB().WithContext(ctx).Create(m).Error; err != nil {
		return errors.Wrap(err, "activity: create")
	}
	return nil
}

func (r *MySQLActivityRepository) Get(ctx context.Context, id string) (*activity.Activity, error) {
	var m FlashActivityModel
	err := r.db.GetDB().WithContext(ctx).Where("id = ?", id).Take(&m).Error
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &activity.NotFoundError{ID: id}
		}
		return nil, errors.Wrap(err, "activity: get")
	}
	return fromModel(&m), nil
}

func (r *MySQLActivityRepository) Update(ctx context.Context, a *activity.Activity) error {
	res := r.db.GetDB().WithContext(ctx).
		Model(&FlashActivityModel{}).
		Where("id = ?", a.ID).
		Updates(map[string]any{
			"name":        a.Name,
			"product_id":  a.ProductID,
			"total_stock": a.TotalStock,
			"start_time":  a.StartTime,
			"end_time":    a.EndTime,
			"status":      string(a.Status),
			"warmup_done": a.WarmupDone,
		})
	if res.Error != nil {
		return errors.Wrap(res.Error, "activity: update")
	}
	if res.RowsAffected == 0 {
		return &activity.NotFoundError{ID: a.ID}
	}
	return nil
}

func toModel(a *activity.Activity) *FlashActivityModel {
	return &FlashActivityModel{
		ID:         a.ID,
		Name:       a.Name,
		ProductID:  a.ProductID,
		TotalStock: a.TotalStock,
		StartTime:  a.StartTime,
		EndTime:    a.EndTime,
		Status:     string(a.Status),
		WarmupDone: a.WarmupDone,
	}
}

func fromModel(m *FlashActivityModel) *activity.Activity {
	return &activity.Activity{
		ID:         m.ID,
		Name:       m.Name,
		ProductID:  m.ProductID,
		TotalStock: m.TotalStock,
		StartTime:  m.StartTime,
		EndTime:    m.EndTime,
		Status:     activity.Status(m.Status),
		WarmupDone: m.WarmupDone,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}
