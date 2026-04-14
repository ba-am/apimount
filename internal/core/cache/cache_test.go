package cache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/apimount/apimount/internal/core/cache"
)

func TestBasicSetGet(t *testing.T) {
	c := cache.New(10*time.Second, 0)
	c.Set("GET:/pets", []byte("hello"))
	val, ok := c.Get("GET:/pets")
	assert.True(t, ok)
	assert.Equal(t, []byte("hello"), val)
}

func TestExpiry(t *testing.T) {
	c := cache.New(50*time.Millisecond, 0)
	c.Set("GET:/pets", []byte("hello"))
	time.Sleep(100 * time.Millisecond)
	_, ok := c.Get("GET:/pets")
	assert.False(t, ok)
}

func TestDisabledCache(t *testing.T) {
	c := cache.New(0, 0)
	c.Set("GET:/pets", []byte("hello"))
	_, ok := c.Get("GET:/pets")
	assert.False(t, ok, "cache with ttl=0 should never return values")
}

func TestInvalidation(t *testing.T) {
	c := cache.New(10*time.Second, 0)
	c.Set("GET:/pets", []byte("pets"))
	c.Set("GET:/pets/1", []byte("pet1"))
	c.Set("GET:/store", []byte("store"))
	c.Invalidate("/pets")
	_, ok1 := c.Get("GET:/pets")
	_, ok2 := c.Get("GET:/pets/1")
	_, ok3 := c.Get("GET:/store")
	assert.False(t, ok1)
	assert.False(t, ok2)
	assert.True(t, ok3)
}

func TestCacheKey(t *testing.T) {
	assert.Equal(t, "GET:/pets", cache.Key("GET", "/pets", nil))
	k := cache.Key("GET", "/pets", map[string]string{"status": "available"})
	assert.Contains(t, k, "GET:/pets:")
	assert.Contains(t, k, "status=available")
}

func TestCacheMiss(t *testing.T) {
	c := cache.New(10*time.Second, 0)
	_, ok := c.Get("nonexistent")
	assert.False(t, ok)
}
