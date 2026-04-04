package processor

import (
	"context"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

// stub
type memoryProcessor struct {
}

func NewMemoryProcessor() *memoryProcessor {
	return &memoryProcessor{}
}

func (m memoryProcessor) CreatePaymentLink(ctx context.Context, order *orderpb.Order) (string, error) {
	return "mem-payment-link", nil
}
