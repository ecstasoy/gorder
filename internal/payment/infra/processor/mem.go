package processor

import (
	"context"

	"github.com/ecstasoy/gorder/common/entity"
)

// stub
type memoryProcessor struct {
}

func NewMemoryProcessor() *memoryProcessor {
	return &memoryProcessor{}
}

func (m memoryProcessor) CreatePaymentLink(ctx context.Context, order *entity.Order) (string, error) {
	return "mem-payment-link", nil
}

func (m memoryProcessor) Refund(_ context.Context, _ string) error {
	return nil
}
