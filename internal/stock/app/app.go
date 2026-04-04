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
	RestoreStock     command.RestoreStockHandler
	WarmUpFlashStock command.WarmUpFlashStockHandler
}

type Queries struct {
	CheckIfItemsInStock query.CheckIfItemsInStockHandler
	GetItems            query.GetItemsHandler
}
