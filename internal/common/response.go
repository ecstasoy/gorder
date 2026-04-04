package common

import (
	"encoding/json"
	"net/http"

	"github.com/ecstasoy/gorder/common/tracing"
	"github.com/gin-gonic/gin"
)

type BaseResponse struct {
}
type response struct {
	Errno   int    `json:"errno"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	TraceID string `json:"trace_id"`
}

func (b *BaseResponse) Response(c *gin.Context, err error, data any) {
	if err != nil {
		b.error(c, err)
	} else {
		b.success(c, data)
	}
}

func (b *BaseResponse) success(c *gin.Context, data any) {
	r := response{
		Errno:   0,
		Message: "success",
		Data:    data,
		TraceID: tracing.TraceID(c.Request.Context()),
	}
	c.JSON(http.StatusOK, r)
	resp, _ := json.Marshal(r)
	c.Set("response", string(resp))
}

func (b *BaseResponse) error(c *gin.Context, err error) {
	r := response{
		Errno:   2,
		Message: err.Error(),
		Data:    nil,
		TraceID: tracing.TraceID(c.Request.Context()),
	}
	c.JSON(http.StatusOK, r)
	resp, _ := json.Marshal(r)
	c.Set("response", string(resp))
}
