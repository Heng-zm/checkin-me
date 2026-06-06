package cache

import (
	"encoding/json"
	"sync"
	"time"
)

type entry struct {
	value     []byte
	expiresAt time.Time
}

// MemoryCache is a tiny process-local TTL cache used for fast dashboard reads.
// It is safe for concurrent HTTP requests and intentionally has no external
// dependency. In multi-instance deployments, keep the TTL short or replace this
// package with a Redis-backed implementation behind the same interface.
type MemoryCache struct {
	mu    sync.RWMutex
	items map[string]entry
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{items: make(map[string]entry)}
}

func (c *MemoryCache) GetJSON(key string, dst any) bool {
	if c == nil || key == "" {
		return false
	}
	now := time.Now()
	c.mu.RLock()
	it, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || now.After(it.expiresAt) {
		if ok {
			c.Delete(key)
		}
		return false
	}
	return json.Unmarshal(it.value, dst) == nil
}

func (c *MemoryCache) SetJSON(key string, value any, ttl time.Duration) {
	if c == nil || key == "" || ttl <= 0 {
		return
	}
	buf, err := json.Marshal(value)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.items[key] = entry{value: buf, expiresAt: time.Now().Add(ttl)}
	c.mu.Unlock()
}

func (c *MemoryCache) Delete(key string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

func (c *MemoryCache) DeletePrefix(prefix string) {
	if c == nil || prefix == "" {
		return
	}
	c.mu.Lock()
	for k := range c.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(c.items, k)
		}
	}
	c.mu.Unlock()
}

func (c *MemoryCache) Size() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}
