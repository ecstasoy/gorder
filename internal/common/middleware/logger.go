package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func StructuredLog(l *logrus.Entry) gin.HandlerFunc {
	return func(c *gin.Context) {
		//t := time.Now()
		//l.WithFields(logrus.Fields{
		//	"request_uri": c.Request.RequestURI,
		//	"client_ip":   c.ClientIP(),
		//	"method":      c.Request.Method,
		//}).Info("request received")
		//c.Next()
		//elapsed := time.Since(t)
		//l.WithFields(logrus.Fields{
		//	"time_elapsed_ms": elapsed,
		//	"request_uri":     c.Request.RequestURI,
		//	"client_ip":       c.ClientIP(),
		//}).Info("request_out")
	}
}
