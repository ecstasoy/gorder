package metrics

import "fmt"

type TodoMetrics struct {
}

func (t TodoMetrics) Inc(_ string, _ int) {
	fmt.Sprintf("TodoMetrics: Inc called with key=%s, value=%d", "key", 1)
}
