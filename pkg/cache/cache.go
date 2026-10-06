/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cache

import (
	"sync"
	"time"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/metrics"
)

// Entry represents a cached entry with expiration
type Entry struct {
	Value      interface{}
	Expiration time.Time
}

// Cache is a generic TTL-based cache
type Cache struct {
	mu        sync.RWMutex
	name      string
	items     map[string]*Entry
	ttl       time.Duration
	nextSweep time.Time
}

// New creates a new cache with the specified TTL, reported in metrics as "default".
func New(ttl time.Duration) *Cache {
	return NewNamed("default", ttl)
}

// NewNamed creates a new cache whose hit and miss metrics carry the given cache label.
func NewNamed(name string, ttl time.Duration) *Cache {
	metrics.CacheHitsTotal.WithLabelValues(name).Add(0)
	metrics.CacheMissesTotal.WithLabelValues(name).Add(0)
	return &Cache{name: name, items: make(map[string]*Entry), ttl: ttl}
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (interface{}, bool) {
	// First, try with read lock for the common case (non-expired entries)
	c.mu.RLock()
	entry, exists := c.items[key]
	if !exists {
		c.mu.RUnlock()
		metrics.CacheMissesTotal.WithLabelValues(c.name).Inc()
		return nil, false
	}

	now := time.Now()
	if !now.After(entry.Expiration) {
		defer c.mu.RUnlock()
		metrics.CacheHitsTotal.WithLabelValues(c.name).Inc()
		return entry.Value, true
	}

	// Entry is expired, upgrade to write lock to clean it up
	c.mu.RUnlock()
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check the entry still exists and is still expired (race condition protection)
	entry, exists = c.items[key]
	if exists && now.After(entry.Expiration) {
		delete(c.items, key)
	}
	metrics.CacheMissesTotal.WithLabelValues(c.name).Inc()
	return nil, false
}

// Set stores a value in the cache with the default TTL
func (c *Cache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL stores a value in the cache with a custom TTL
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if !now.Before(c.nextSweep) {
		c.removeExpiredLocked(now)
		c.nextSweep = now.Add(min(time.Minute, max(time.Millisecond, c.ttl/2)))
	}
	c.items[key] = &Entry{
		Value:      value,
		Expiration: time.Now().Add(ttl),
	}
}

// Delete removes a key from the cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Has checks if a key exists in the cache (and is not expired) without counting as a hit or miss.
func (c *Cache) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, exists := c.items[key]
	return exists && !time.Now().After(entry.Expiration)
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeExpiredLocked(time.Now())
	return len(c.items)
}

// Clear removes all items from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*Entry)
}

// Stop is a no-op; expired entries are swept on write, so the cache owns no goroutine.
func (c *Cache) Stop() {}

func (c *Cache) removeExpiredLocked(now time.Time) {
	for key, entry := range c.items {
		if now.After(entry.Expiration) {
			delete(c.items, key)
		}
	}
}

// GetOrSet retrieves a value from cache or sets it using the provided function
func (c *Cache) GetOrSet(key string, fetchFunc func() (interface{}, error)) (interface{}, error) {
	// First, try to get from cache
	if value, exists := c.Get(key); exists {
		return value, nil
	}

	// Not in cache, fetch the value
	value, err := fetchFunc()
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.Set(key, value)
	return value, nil
}

// GetOrSetWithTTL retrieves a value from cache or sets it using the provided function with custom TTL
func (c *Cache) GetOrSetWithTTL(key string, ttl time.Duration, fetchFunc func() (interface{}, error)) (interface{}, error) {
	// First, try to get from cache
	if value, exists := c.Get(key); exists {
		return value, nil
	}

	// Not in cache, fetch the value
	value, err := fetchFunc()
	if err != nil {
		return nil, err
	}

	// Store in cache with custom TTL
	c.SetWithTTL(key, value, ttl)
	return value, nil
}
