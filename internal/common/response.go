package common

import (
	"encoding/json"
	"net/http"

	"github.com/ecstasoy/gorder/common/handler/errors"
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
	errno, errmsg := errors.Output(nil)
	r := response{
		Errno:   errno,
		Message: errmsg,
		Data:    data,
		TraceID: tracing.TraceID(c.Request.Context()),
	}
	resp, _ := json.Marshal(r)
	c.Set("response", string(resp))
	c.JSON(http.StatusOK, r)
}

func (b *BaseResponse) error(c *gin.Context, err error) {
	errno, errmsg := errors.Output(err)
	r := response{
		Errno:   errno,
		Message: errmsg,
		Data:    nil,
		TraceID: tracing.TraceID(c.Request.Context()),
	}
	resp, _ := json.Marshal(r)
	c.Set("response", string(resp))
	c.JSON(http.StatusOK, r)
}
