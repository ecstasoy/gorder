package adapters

import (
	"context"

	"github.com/ecstasoy/gorder/common/entity"
	"github.com/ecstasoy/gorder/stock/domain/reservation"
	"github.com/ecstasoy/gorder/stock/infra/persistent"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReservationModel 映射 o_stock_reservation 表 (见 init.sql)。
type ReservationModel struct {
	ID         uint   `gorm:"primaryKey"`
	OrderID    string `gorm:"column:order_id"`
	ProductID  string `gorm:"column:product_id"`
	Quantity   int32  `gorm:"column:quantity"`
	Status     string `gorm:"column:status"` // held / confirmed / released
	CreatedAt  string `gorm:"column:created_at"`
	UpdatedAt  string `gorm:"column:updated_at"`
}

func (ReservationModel) TableName() string { return "o_stock_reservation" }

const (
	statusHeld      = "held"
	statusConfirmed = "confirmed"
	statusReleased  = "released"
)

type MySQLReservationRepository struct {
	db *persistent.MySQL
}

func NewMySQLReservationRepository(db *persistent.MySQL) *MySQLReservationRepository {
	return &MySQLReservationRepository{db: db}
}

// Reserve 在一个事务里 (1) 幂等地写 reservation 行 (status=held) (2) 从
// o_stock 扣对应 quantity。任一 item 库存不足 → 整个事务回滚,没有 item
// 被部分扣减。同 OrderID 重复 Reserve 是 no-op (existing held)。
func (r MySQLReservationRepository) Reserve(ctx context.Context, orderID string, items []*entity.ItemWithQuantity) error {
	return r.db.StartTransaction(func(tx *gorm.DB) error {
		for _, item := range items {
			// 1. 尝试 INSERT IGNORE —— RowsAffected 0 表示行已存在 (uk_order_product),
			//    1 表示新插入。
			res := tx.WithContext(ctx).Exec(
				"INSERT IGNORE INTO o_stock_reservation (order_id, product_id, quantity, status) VALUES (?, ?, ?, ?)",
				orderID, item.ID, item.Quantity, statusHeld,
			)
			if res.Error != nil {
				return errors.Wrapf(res.Error, "reservation insert for order=%s product=%s", orderID, item.ID)
			}

			if res.RowsAffected == 0 {
				// 已有 row —— 锁住并检查 status。
				var existing ReservationModel
				if err := tx.WithContext(ctx).
					Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
					Where("order_id = ? AND product_id = ?", orderID, item.ID).
					Take(&existing).Error; err != nil {
					return errors.Wrapf(err, "reservation lock for order=%s product=%s", orderID, item.ID)
				}
				if existing.Status == statusHeld {
					continue // idempotent
				}
				return reservation.ConflictError{OrderID: orderID, Current: existing.Status, Wanted: "reserve"}
			}

			// 2. 新行 —— 扣 stock。CAS 防过卖 (quantity >= want)。
			res = tx.WithContext(ctx).Exec(
				"UPDATE o_stock SET quantity = quantity - ? WHERE product_id = ? AND quantity >= ?",
				item.Quantity, item.ID, item.Quantity,
			)
			if res.Error != nil {
				return errors.Wrapf(res.Error, "stock deduct for product=%s", item.ID)
			}
			if res.RowsAffected == 0 {
				return reservation.InsufficientStockError{ProductID: item.ID, Want: item.Quantity}
			}
		}
		return nil
	})
}

// Confirm 把 OrderID 名下所有 held 行改为 confirmed。已 confirmed 是 no-op;
// 任何行处于 released 状态 → ConflictError。没有任何行 → NotFoundError。
func (r MySQLReservationRepository) Confirm(ctx context.Context, orderID string) error {
	return r.db.StartTransaction(func(tx *gorm.DB) error {
		var rows []ReservationModel
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Where("order_id = ?", orderID).
			Find(&rows).Error; err != nil {
			return errors.Wrapf(err, "reservation fetch for order=%s", orderID)
		}
		if len(rows) == 0 {
			return reservation.NotFoundError{OrderID: orderID}
		}

		for _, row := range rows {
			switch row.Status {
			case statusHeld, statusConfirmed: // confirmed 是 idempotent
			case statusReleased:
				return reservation.ConflictError{OrderID: orderID, Current: row.Status, Wanted: "confirm"}
			}
		}

		return tx.WithContext(ctx).Exec(
			"UPDATE o_stock_reservation SET status = ? WHERE order_id = ? AND status = ?",
			statusConfirmed, orderID, statusHeld,
		).Error
	})
}

// Release 把 OrderID 名下所有 held 行改为 released,并把对应数量还回 o_stock。
// 已 released 是 no-op (整个 OrderID 都已 released 时整体 no-op)。任何行已
// confirmed → ConflictError (实扣后只能走 refund,不能 release)。没有任何行
// → no-op (从未 Reserve 过的订单 release 是合法的)。
func (r MySQLReservationRepository) Release(ctx context.Context, orderID string) error {
	return r.db.StartTransaction(func(tx *gorm.DB) error {
		var rows []ReservationModel
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Where("order_id = ?", orderID).
			Find(&rows).Error; err != nil {
			return errors.Wrapf(err, "reservation fetch for order=%s", orderID)
		}
		if len(rows) == 0 {
			return nil // no-op
		}

		for _, row := range rows {
			switch row.Status {
			case statusHeld:
				// 还库存
				if err := tx.WithContext(ctx).Exec(
					"UPDATE o_stock SET quantity = quantity + ? WHERE product_id = ?",
					row.Quantity, row.ProductID,
				).Error; err != nil {
					return errors.Wrapf(err, "stock restore for product=%s", row.ProductID)
				}
			case statusReleased: // idempotent
			case statusConfirmed:
				return reservation.ConflictError{OrderID: orderID, Current: row.Status, Wanted: "release"}
			}
		}

		return tx.WithContext(ctx).Exec(
			"UPDATE o_stock_reservation SET status = ? WHERE order_id = ? AND status = ?",
			statusReleased, orderID, statusHeld,
		).Error
	})
}
