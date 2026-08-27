package factory

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// TestSingletonConcurrentGet hammers Get from many goroutines across many keys.
// While one goroutine writes s.cache[key] under the lock, others read the map
// in the fast path. Under `go test -race` this reproduces the concurrent map
// read/write data race that an unlocked first read introduces.
func TestSingletonConcurrentGet(t *testing.T) {
	const (
		goroutines = 50
		keys       = 50
	)
	s := NewSingleton(func(key string) any { return key })

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range goroutines {
		wg.Go(func() {
			<-start // release all goroutines at once to maximise contention
			for k := range keys {
				key := strconv.Itoa(k)
				if got := s.Get(key); got != key {
					t.Errorf("Get(%q) = %v, want %q", key, got, key)
				}
			}
		})
	}
	close(start)
	wg.Wait()
}

// TestSingletonSupplierRunsOncePerKey locks in the contract the only caller
// (handler/redis) depends on: the supplier — which builds an expensive
// *redis.Client — must run exactly once per key even under heavy contention.
func TestSingletonSupplierRunsOncePerKey(t *testing.T) {
	const goroutines = 100
	var calls int64
	s := NewSingleton(func(key string) any {
		atomic.AddInt64(&calls, 1)
		return key
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range goroutines {
		wg.Go(func() {
			<-start
			s.Get("only")
		})
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("supplier called %d times, want exactly 1", got)
	}
}

// TestSingletonGetReturnsCachedValue is a basic single-threaded sanity check
// that the same instance is returned across calls for the same key.
func TestSingletonGetReturnsCachedValue(t *testing.T) {
	var calls int64
	s := NewSingleton(func(key string) any {
		atomic.AddInt64(&calls, 1)
		return new(int)
	})

	first := s.Get("k")
	second := s.Get("k")
	if first != second {
		t.Fatalf("Get returned different instances for the same key: %p vs %p", first, second)
	}
	if calls != 1 {
		t.Fatalf("supplier called %d times, want 1", calls)
	}
}
