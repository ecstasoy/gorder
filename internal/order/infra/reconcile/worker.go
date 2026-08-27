// Package reconcile 是 reservation 的最终对账兜底。saga 在 ConfirmOrder /
// CancelOrder 失败时只记日志(stock.Confirm / stock.Release 是 best-effort
// 阶段 2),这意味着极少数情况下会出现 Mongo Order 已到终态 (PAID /
// CANCELLED) 但 MySQL o_stock_reservation 还停在 held 的孤儿态。worker
// 定期扫这种孤儿态并补 Confirm / Release —— ADR-0001 留下的 follow-up
// (见 docs/adr-0001-summary.md "后续工作 #5") 在此收口。
//
// 设计要点:
//   - 放在 order 侧而非 stock 侧。order 已经持 stockGRPC + Mongo Repository,
//     stock-side worker 反而需要新增 stock→order gRPC 反向依赖。
//   - 不用 Mongo schema 新字段。利用 ObjectID 4-byte 时间戳前缀做时间过滤。
//   - Confirm / Release 本身就幂等 (stock-side adapter 已实现),worker 不需
//     要"是否已对账"状态。每 tick 扫一批,已对账的就是 no-op,代价仅 gRPC
//     往返。
//   - 不做 SLO:这是兜底机制,不是热路径。延迟分钟级即可。
package reconcile

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
	domain "github.com/ecstasoy/gorder/order/domain/order"
	"github.com/sirupsen/logrus"
)

// StockOps 是 worker 唯一需要的 stockGRPC 子集 —— 收窄依赖。
// adapters/grpc.StockGRPC 已经满足这个接口。
type StockOps interface {
	Confirm(ctx context.Context, orderID string) error
	Release(ctx context.Context, orderID string) error
}

type Config struct {
	// Interval 是扫描间隔。建议生产 5min,测试可设小到 10s。
	Interval time.Duration
	// MinAge 是订单进入终态后等多久才被视作"应该已经对账完了"。
	// 必须大于 saga 通常耗时 + RabbitMQ 重试上限,默认 5min 是保守值。
	MinAge time.Duration
	// MaxAge 是扫描上限。超过这个年龄的孤儿不再自动处理,留给人工告警。
	// 默认 24h —— 设这条是避免 worker 启动时扫到几个月前的历史数据狂调 gRPC。
	MaxAge time.Duration
	// BatchLimit 是每 tick 处理的最多订单数。控制单次 tick 耗时和 stock 服务压力。
	BatchLimit int64
}

func DefaultConfig() Config {
	return Config{
		Interval:   5 * time.Minute,
		MinAge:     5 * time.Minute,
		MaxAge:     24 * time.Hour,
		BatchLimit: 50,
	}
}

type Worker struct {
	repo  domain.Repository
	stock StockOps
	cfg   Config
}

func NewWorker(repo domain.Repository, stock StockOps, cfg Config) *Worker {
	if repo == nil {
		panic("reconcile.NewWorker: nil repo")
	}
	if stock == nil {
		panic("reconcile.NewWorker: nil stock")
	}
	if cfg.Interval <= 0 || cfg.MinAge <= 0 || cfg.MaxAge <= 0 || cfg.BatchLimit <= 0 {
		panic("reconcile.NewWorker: invalid config")
	}
	return &Worker{repo: repo, stock: stock, cfg: cfg}
}

// Run 阻塞循环,ctx 取消时退出。建议作为 goroutine 启动。
func (w *Worker) Run(ctx context.Context) {
	logrus.WithFields(logrus.Fields{
		"interval":    w.cfg.Interval,
		"min_age":     w.cfg.MinAge,
		"max_age":     w.cfg.MaxAge,
		"batch_limit": w.cfg.BatchLimit,
	}).Info("reservation reconcile worker started")
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logrus.Info("reservation reconcile worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	now := time.Now()
	cutoff := now.Add(-w.cfg.MinAge)
	lowerBound := now.Add(-w.cfg.MaxAge)

	orders, err := w.repo.ListTerminalOlderThan(ctx, cutoff, w.cfg.BatchLimit)
	if err != nil {
		logrus.WithError(err).Warn("reconcile: list terminal failed")
		return
	}
	if len(orders) == 0 {
		return
	}

	var confirmed, released, skipped, failed int
	for _, o := range orders {
		// 跳过太老的 —— 避免历史数据被反复扫到。
		if createdAt, ok := orderCreatedAt(o.ID); ok && createdAt.Before(lowerBound) {
			skipped++
			continue
		}
		switch o.Status {
		case orderpb.OrderStatus_ORDER_STATUS_PAID:
			if err := w.stock.Confirm(ctx, o.ID); err != nil {
				if isStockNotFound(err) {
					skipped++
					continue
				}
				failed++
				logrus.WithError(err).WithField("order_id", o.ID).
					Warn("reconcile: stock.Confirm failed (will retry next tick)")
				continue
			}
			confirmed++
		case orderpb.OrderStatus_ORDER_STATUS_CANCELLED:
			if err := w.stock.Release(ctx, o.ID); err != nil {
				failed++
				logrus.WithError(err).WithField("order_id", o.ID).
					Warn("reconcile: stock.Release failed (will retry next tick)")
				continue
			}
			released++
		default:
			skipped++
		}
	}

	if confirmed+released+failed > 0 {
		logrus.WithFields(logrus.Fields{
			"scanned":   len(orders),
			"confirmed": confirmed,
			"released":  released,
			"skipped":   skipped,
			"failed":    failed,
		}).Info("reconcile tick")
	}
}

// orderCreatedAt 利用 Mongo ObjectID 的 4-byte 时间戳前缀提取创建时间。
// 如果 ID 不是合法 hex (理论上不会出现) 返回 false。
func orderCreatedAt(hexID string) (time.Time, bool) {
	if len(hexID) < 8 {
		return time.Time{}, false
	}
	// ObjectID 前 4 byte = 8 hex char = unix 秒时间戳 (big-endian)。
	var secs uint32
	for i := 0; i < 8; i += 2 {
		hi, ok1 := fromHex(hexID[i])
		lo, ok2 := fromHex(hexID[i+1])
		if !ok1 || !ok2 {
			return time.Time{}, false
		}
		secs = (secs << 8) | uint32(hi<<4|lo)
	}
	return time.Unix(int64(secs), 0), true
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// isStockNotFound 识别 reservation 不存在的情况 —— 老订单可能根本没经过
// ADR-0001 的 Reserve 路径,worker 不该把 NotFound 当 failure 重试。
func isStockNotFound(err error) bool {
	if err == nil {
		return false
	}
	// gRPC 错误信息里通常带 "not found" 字样;严格类型断言需要跨服务共享
	// domain error,代价不抵这个 best-effort 兜底的收益。
	type notFounder interface{ NotFound() bool }
	var nf notFounder
	if stderrors.As(err, &nf) {
		return nf.NotFound()
	}
	return false
}
