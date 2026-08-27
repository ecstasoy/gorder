package factory

import "sync"

type Supplier func(string) any

type Singleton struct {
	cache    map[string]any
	locker   sync.RWMutex
	supplier Supplier
}

func NewSingleton(supplier Supplier) *Singleton {
	return &Singleton{
		cache:    make(map[string]any),
		supplier: supplier,
	}
}

func (s *Singleton) Get(key string) any {
	// Fast path: a read lock is enough to safely observe the map concurrently.
	s.locker.RLock()
	value, hit := s.cache[key]
	s.locker.RUnlock()
	if hit {
		return value
	}

	// Slow path: take the write lock and re-check before running the supplier,
	// so the supplier runs at most once per key under contention.
	s.locker.Lock()
	defer s.locker.Unlock()

	if value, hit := s.cache[key]; hit {
		return value
	}

	s.cache[key] = s.supplier(key)
	return s.cache[key]
}
