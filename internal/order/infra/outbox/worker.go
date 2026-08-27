package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ecstasoy/gorder/common/broker"
	"github.com/sirupsen/logrus"
)

const (
	defaultPollInterval = 500 * time.Millisecond
	defaultMaxAttempts  = 5
)

// Worker 是 outbox 的后台 publisher，按 ADR-0001 在 order 进程内单实例运行。
// order 进程崩溃时 outbox 暂停 —— 事件不丢但延迟到进程恢复。
type Worker struct {
	repo         *MongoOutboxRepo
	publisher    broker.Publisher
	pollInterval time.Duration
	maxAttempts  int
}

func NewWorker(repo *MongoOutboxRepo, publisher broker.Publisher) *Worker {
	return &Worker{
		repo:         repo,
		publisher:    publisher,
		pollInterval: defaultPollInterval,
		maxAttempts:  defaultMaxAttempts,
	}
}

// Run 阻塞循环，ctx 取消时退出。建议作为 goroutine 启动。
func (w *Worker) Run(ctx context.Context) {
	logrus.Info("outbox worker started")
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logrus.Info("outbox worker stopped")
			return
		case <-ticker.C:
			w.drain(ctx)
		}
	}
}

// drain 把当前可拿的 pending 记录一次消化完，直到 ClaimNext 返回 nil 或出错。
func (w *Worker) drain(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		rec, err := w.repo.ClaimNext(ctx)
		if err != nil {
			logrus.WithError(err).Warn("outbox: ClaimNext failed")
			return
		}
		if rec == nil {
			return
		}
		w.process(ctx, rec)
	}
}

func (w *Worker) process(ctx context.Context, rec *Record) {
	if err := w.dispatch(ctx, rec); err != nil {
		terminal := rec.Attempts >= w.maxAttempts
		if markErr := w.repo.MarkFailed(ctx, rec.MongoID, err.Error(), terminal); markErr != nil {
			logrus.WithError(markErr).WithField("event_id", rec.EventID).
				Warn("outbox: MarkFailed update failed")
		}
		logrus.WithError(err).
			WithFields(logrus.Fields{
				"event_id": rec.EventID,
				"attempts": rec.Attempts,
				"terminal": terminal,
			}).
			Warn("outbox: publish failed")
		return
	}
	if err := w.repo.MarkSent(ctx, rec.MongoID); err != nil {
		logrus.WithError(err).WithField("event_id", rec.EventID).
			Warn("outbox: MarkSent failed (event was already published; downstream must dedup by event_id)")
	}
}

func (w *Worker) dispatch(ctx context.Context, rec *Record) error {
	// 从持久化的 trace context 续接原始下单 trace,让异步发布挂在原 trace 下,
	// 而不是新开 root span (G-3)。publisher 内部会把这份 ctx 注入 AMQP header,
	// 下游消费者 Extract 后即成为原 trace 的子 span。
	if len(rec.TraceContext) > 0 {
		headers := make(map[string]any, len(rec.TraceContext))
		for k, v := range rec.TraceContext {
			headers[k] = v
		}
		ctx = broker.ExtractRabbitMQHeaders(ctx, headers)
	}
	event := broker.DomainEvent{
		Dest: rec.Dest,
		// json.RawMessage 的 MarshalJSON 直接输出原 bytes，Publisher 内部 json.Marshal
		// 不会 double-encode。
		Data: json.RawMessage(rec.Payload),
	}
	switch rec.Kind {
	case KindQueue:
		return w.publisher.Publish(ctx, event)
	case KindExchange:
		return w.publisher.Broadcast(ctx, event)
	case KindDelayed:
		return w.publisher.PublishDelayed(ctx, event)
	default:
		return fmt.Errorf("outbox: unknown kind %q", rec.Kind)
	}
}
