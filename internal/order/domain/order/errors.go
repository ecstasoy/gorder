package order

import (
	"fmt"

	"github.com/ecstasoy/gorder/common/genproto/orderpb"
)

type StatusConflictError struct {
	OrderID       string
	CurrentStatus orderpb.OrderStatus
	TargetStatus  orderpb.OrderStatus
}

func (e *StatusConflictError) Error() string {
	return fmt.Sprintf("cannot transit order %s from %s to %s",
		e.OrderID, e.CurrentStatus.String(), e.TargetStatus.String())
}
