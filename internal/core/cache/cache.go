package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Cache is an in-memory TTL cache for HTTP GET responses.
// Key format: "METHOD:URL:querystring"
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*entry
	maxSize int64
	curSize int64
	ttl     time.Duration
}

type entry struct {
	value     []byte
	expiresAt time.Time
	size      int64
}

// New creates a new Cache with the given TTL and max size in bytes.
func New(ttl time.Duration, maxSizeBytes int64) *Cache {
	return &Cache{
		entries: make(map[string]*entry),
		maxSize: maxSizeBytes,
		ttl:     ttl,
	}
}

// Key builds a cache key from method, URL, and query params.
func Key(method, url string, queryParams map[string]string) string {
	if len(queryParams) == 0 {
		return fmt.Sprintf("%s:%s", method, url)
	}
	parts := make([]string, 0, len(queryParams))
	for k, v := range queryParams {
		parts = append(parts, k+"="+v)
	}
	return fmt.Sprintf("%s:%s:%s", method, url, strings.Join(parts, "&"))
}

// Get retrieves a cached value. Returns (value, true) if found and not expired.
func (c *Cache) Get(key string) ([]byte, bool) {
	if c.ttl == 0 {
		return nil, false
	}
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.curSize -= e.size
		c.mu.Unlock()
		return nil, false
	}
	return e.value, true
}

// Set stores a value in the cache.
func (c *Cache) Set(key string, value []byte) {
	if c.ttl == 0 {
		return
	}
	size := int64(len(value))
	if c.maxSize > 0 && size > c.maxSize {
		return // single item too large
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if old, ok := c.entries[key]; ok {
		c.curSize -= old.size
	}

	for c.maxSize > 0 && c.curSize+size > c.maxSize {
		c.evictOne()
	}

	c.entries[key] = &entry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
		size:      size,
	}
	c.curSize += size
}

// Invalidate removes all cache entries whose keys contain the given prefix.
func (c *Cache) Invalidate(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.entries {
		if strings.Contains(key, prefix) {
			c.curSize -= e.size
			delete(c.entries, key)
		}
	}
}

// evictOne removes the first expired entry found, or any entry if none expired.
// Must be called with c.mu held.
func (c *Cache) evictOne() {
	now := time.Now()
	for key, e := range c.entries {
		if now.After(e.expiresAt) {
			c.curSize -= e.size
			delete(c.entries, key)
			return
		}
	}
	for key, e := range c.entries {
		c.curSize -= e.size
		_ = e
		delete(c.entries, key)
		return
	}
}

// StartEviction starts a background goroutine that periodically removes expired entries.
func (c *Cache) StartEviction() {
	if c.ttl == 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(c.ttl / 2)
		for range ticker.C {
			c.evictExpired()
		}
	}()
}

func (c *Cache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.entries {
		if now.After(e.expiresAt) {
			c.curSize -= e.size
			delete(c.entries, key)
		}
	}
}

// Len returns the number of entries in the cache.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
