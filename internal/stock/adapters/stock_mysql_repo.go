package adapters

import (
	"context"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/stock/infra/persistent"
	"github.com/ecstasoy/gorder/stock/infra/persistent/builder"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type MySQLStockRepository struct {
	db *persistent.MySQL
}

func NewMySQLStockRepository(db *persistent.MySQL) *MySQLStockRepository {
	return &MySQLStockRepository{db: db}
}

func (m MySQLStockRepository) GetStock(ctx context.Context, ids []string) ([]*entity.ItemWithQuantity, error) {
	data, err := m.db.BatchGetStockByID(ctx, builder.NewStock().ProductIDs(ids...))
	if err != nil {
		return nil, errors.Wrap(err, "BatchGetStockByID error")
	}
	var result []*entity.ItemWithQuantity
	for _, d := range data {
		result = append(result, &entity.ItemWithQuantity{
			ID:       d.ProductID,
			Quantity: d.Quantity,
		})
	}
	return result, nil
}

func (m MySQLStockRepository) UpdateStock(
	ctx context.Context,
	data []*entity.ItemWithQuantity,
	updateFunc func(
		ctx context.Context,
		existing []*entity.ItemWithQuantity,
		query []*entity.ItemWithQuantity,
	) ([]*entity.ItemWithQuantity, error),
) error {
	return m.db.StartTransaction(func(tx *gorm.DB) (err error) {
		defer func() {
			if err != nil {
				logrus.Warnf("update stock transaction err=%v", err)
			}
		}()
		err = m.updatePessimistic(ctx, tx, data, updateFunc)
		// err = m.updateOptimistic(ctx, tx, data, updateFunc)
		return err
	})
}

func (m MySQLStockRepository) updateOptimistic(
	ctx context.Context,
	tx *gorm.DB,
	data []*entity.ItemWithQuantity,
	updateFn func(ctx context.Context, existing []*entity.ItemWithQuantity, query []*entity.ItemWithQuantity,
	) ([]*entity.ItemWithQuantity, error)) error {
	for _, queryData := range data {
		var newestRecord *persistent.StockModel
		newestRecord, err := m.db.GetStockByID(ctx, builder.NewStock().ProductIDs(queryData.ID))
		if err != nil {
			return err
		}
		if err = m.db.BatchUpdateStock(
			ctx,
			tx,
			builder.NewStock().ProductIDs(queryData.ID).Versions(newestRecord.Version).QuantityGT(queryData.Quantity),
			map[string]any{
				"quantity": gorm.Expr("quantity - ?", queryData.Quantity),
				"version":  newestRecord.Version + 1,
			}); err != nil {
			return err
		}
	}

	return nil
}

func (m MySQLStockRepository) unmarshalFromDatabase(dest []persistent.StockModel) []*entity.ItemWithQuantity {
	var result []*entity.ItemWithQuantity
	for _, i := range dest {
		result = append(result, &entity.ItemWithQuantity{
			ID:       i.ProductID,
			Quantity: i.Quantity,
		})
	}
	return result
}

func (m MySQLStockRepository) updatePessimistic(
	ctx context.Context,
	tx *gorm.DB,
	data []*entity.ItemWithQuantity,
	updateFunc func(ctx context.Context, existing []*entity.ItemWithQuantity, query []*entity.ItemWithQuantity,
	) ([]*entity.ItemWithQuantity, error)) error {

	var dest []persistent.StockModel
	dest, err := m.db.BatchGetStockByID(ctx, builder.NewStock().ProductIDs(getIDFromEntities(data)...).ForUpdate())
	if err != nil {
		return errors.Wrap(err, "failed to find data")
	}

	existing := m.unmarshalFromDatabase(dest)
	updated, err := updateFunc(ctx, existing, data)
	if err != nil {
		return err
	}

	for _, upd := range updated {
		for _, query := range data {
			if upd.ID != query.ID {
				continue
			}
			if err = m.db.BatchUpdateStock(ctx, tx, builder.NewStock().ProductIDs(upd.ID).QuantityGT(query.Quantity),
				map[string]any{"quantity": gorm.Expr("quantity - ?", query.Quantity)}); err != nil {
				return errors.Wrapf(err, "unable to update %s", upd.ID)
			}
		}
	}
	return nil
}

func (m MySQLStockRepository) RestoreStock(ctx context.Context, items []*entity.ItemWithQuantity) error {
	return m.db.StartTransaction(func(tx *gorm.DB) error {
		var dest []persistent.StockModel
		dest, err := m.db.BatchGetStockByID(ctx, builder.NewStock().ProductIDs(getIDFromEntities(items)...).ForUpdate())
		if err != nil {
			return errors.Wrap(err, "RestoreStock: failed to fetch stock")
		}
		existing := m.unmarshalFromDatabase(dest)
		_ = existing // 已锁行，直接加回即可
		for _, item := range items {
			if err := m.db.BatchUpdateStock(ctx, tx,
				builder.NewStock().ProductIDs(item.ID),
				map[string]any{"quantity": gorm.Expr("quantity + ?", item.Quantity)},
			); err != nil {
				return errors.Wrapf(err, "RestoreStock: failed to restore %s", item.ID)
			}
		}
		return nil
	})
}

func getIDFromEntities(items []*entity.ItemWithQuantity) []string {
	var ids []string
	for _, i := range items {
		ids = append(ids, i.ID)
	}
	return ids
}

// UpsertStock 把指定 product_id 的 quantity SET 为给定值。秒杀 warmup 用,
// 把 flash SKU 行的库存重置为本场活动总量。row 必须已存在(init.sql 里 seed)。
func (m MySQLStockRepository) UpsertStock(ctx context.Context, items []*entity.ItemWithQuantity) error {
	return m.db.StartTransaction(func(tx *gorm.DB) error {
		for _, item := range items {
			// 先确认 row 存在(不能用 UPDATE 的 RowsAffected 判断,
			// MySQL 默认对"值未变化"的 UPDATE 也返回 RowsAffected=0,
			// 会和"row 不存在"无法区分)
			var count int64
			if err := tx.WithContext(ctx).
				Model(&persistent.StockModel{}).
				Where("product_id = ?", item.ID).
				Count(&count).Error; err != nil {
				return errors.Wrapf(err, "UpsertStock: count failed for %s", item.ID)
			}
			if count == 0 {
				return errors.Errorf("UpsertStock: product_id %s not seeded in o_stock", item.ID)
			}
			if err := tx.WithContext(ctx).
				Model(&persistent.StockModel{}).
				Where("product_id = ?", item.ID).
				UpdateColumn("quantity", item.Quantity).Error; err != nil {
				return errors.Wrapf(err, "UpsertStock: update failed for %s", item.ID)
			}
		}
		return nil
	})
}

// DeductStock 纯 CAS 扣减,不查 Stripe 元数据。秒杀消费者(已经从 flash:meta
// 拿到价格)和未来任何"只想扣库存"的场景都用它,替代 CheckIfItemsInStock 的
// "查 Stripe + 扣库存"组合。
func (m MySQLStockRepository) DeductStock(ctx context.Context, items []*entity.ItemWithQuantity) error {
	return m.db.StartTransaction(func(tx *gorm.DB) error {
		for _, item := range items {
			res := tx.WithContext(ctx).
				Model(&persistent.StockModel{}).
				Where("product_id = ? AND quantity >= ?", item.ID, item.Quantity).
				UpdateColumn("quantity", gorm.Expr("quantity - ?", item.Quantity))
			if res.Error != nil {
				return errors.Wrapf(res.Error, "DeductStock: failed for %s", item.ID)
			}
			if res.RowsAffected == 0 {
				return errors.Errorf("DeductStock: insufficient stock for %s (want %d)", item.ID, item.Quantity)
			}
		}
		return nil
	})
}
