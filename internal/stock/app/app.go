package app

import (
	"github.com/ecstasoy/gorder/stock/app/command"
	"github.com/ecstasoy/gorder/stock/app/query"
)

type Application struct {
	Commands Commands
	Queries  Queries
}

type Commands struct {
	WarmUpFlashStock command.WarmUpFlashStockHandler
	// ADR-0001 Step 4: reservation lifecycle. Replaces the pre-ADR
	// DeductStock + RestoreStock which were removed in Step 7.
	ReserveStock command.ReserveStockHandler
	ConfirmStock command.ConfirmStockHandler
	ReleaseStock command.ReleaseStockHandler
}

type Queries struct {
	GetItems query.GetItemsHandler
}
