package cache

import (
	"context"
	"sync"
	"time"
)

// MemoryCache — потокобезопасный in-memory кеш для unit-тестов. Не для прода:
// нет eviction'а, нет ограничения по памяти.
type MemoryCache struct {
	mu    sync.RWMutex
	fresh map[string]memEntry
	stale map[string]memEntry
}

type memEntry struct {
	val       []byte
	expiresAt time.Time
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		fresh: make(map[string]memEntry),
		stale: make(map[string]memEntry),
	}
}

func (c *MemoryCache) Get(_ context.Context, key string) ([]byte, error) {
	return c.read(c.fresh, key)
}

func (c *MemoryCache) GetStale(_ context.Context, key string) ([]byte, error) {
	return c.read(c.stale, key)
}

func (c *MemoryCache) Set(_ context.Context, key string, val []byte, freshTTL, staleTTL time.Duration) error {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fresh[key] = memEntry{val: val, expiresAt: now.Add(freshTTL)}
	c.stale[key] = memEntry{val: val, expiresAt: now.Add(staleTTL)}
	return nil
}

func (c *MemoryCache) read(store map[string]memEntry, key string) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := store[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, ErrCacheMiss
	}
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, nil
}
