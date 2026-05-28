package server

import (
	"github.com/ecstasoy/gorder/common/middleware"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func RunHTTPServer(serviceName string, wrapper func(router *gin.Engine)) {
	addr := viper.Sub(serviceName).GetString("http-addr")
	if addr == "" {
		panic("http-addr is not defined in config")
	}
	RunHTTPServerOnAddr(addr, wrapper)
}

func RunHTTPServerOnAddr(addr string, wrapper func(router *gin.Engine)) {
	apiRouter := gin.New()
	setMiddlewares(apiRouter)
	wrapper(apiRouter)
	registerAdminRoutes(apiRouter)
	apiRouter.Group("/api")
	if err := apiRouter.Run(addr); err != nil {
		panic(err)
	}
}

func RunAdminHTTPServer(addr string) {
	if addr == "" {
		panic("admin http addr is empty")
	}
	r := gin.New()
	r.Use(gin.Recovery())
	registerAdminRoutes(r)
	logrus.Infof("Starting admin HTTP server on %s", addr)
	if err := r.Run(addr); err != nil {
		logrus.Panicf("admin http server failed: %v", err)
	}
}

func setMiddlewares(r *gin.Engine) {
	//r.Use(middleware.StructuredLog(logrus.NewEntry(logrus.StandardLogger())))
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLog(logrus.NewEntry(logrus.StandardLogger())))
	r.Use(otelgin.Middleware("default-http-server"))
	r.Use(middleware.PrometheusMetrics())
}

func registerAdminRoutes(r *gin.Engine) {
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/health", livenessHandler)
	r.GET("/ready", readinessHandler)
}
