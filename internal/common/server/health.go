package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ReadyCheck func(context.Context) error

var (
	readyMu     sync.RWMutex
	readyChecks = map[string]ReadyCheck{}
)

func RegisterReadyCheck(name string, check ReadyCheck) {
	readyMu.Lock()
	defer readyMu.Unlock()
	readyChecks[name] = check
}

func livenessHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func readinessHandler(c *gin.Context) {
	readyMu.RLock()
	snapshot := make(map[string]ReadyCheck, len(readyChecks))
	for k, v := range readyChecks {
		snapshot[k] = v
	}
	readyMu.RUnlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	results := gin.H{}
	failed := false
	for name, check := range snapshot {
		if err := check(ctx); err != nil {
			results[name] = err.Error()
			failed = true
		} else {
			results[name] = "ok"
		}
	}
	status := http.StatusOK
	body := "ok"
	if failed {
		status = http.StatusServiceUnavailable
		body = "not ready"
	}
	c.JSON(status, gin.H{"status": body, "checks": results})
}
