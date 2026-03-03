package main

import (
	"github.com/ecstasoy/gorder/stock/app"
	"github.com/gin-gonic/gin"
)

type HTTPServer struct {
	app app.Application
}

func (H HTTPServer) PostItems(c *gin.Context) {
	//TODO implement me
	panic("implement me")
}

func (H HTTPServer) PostItemsCheck(c *gin.Context) {
	//TODO implement me
	panic("implement me")
}
