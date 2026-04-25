// Package cache provides a thread-safe in-memory TTL cache for GCP resource data.
// Each entry expires independently; a Get on an expired entry returns (nil, false).
// The cache is intentionally simple: there is no background eviction goroutine —
// stale entries are evicted lazily on Get and on explicit Purge calls.
package cache

import (
	"sync"
	"time"

	"github.com/brekol/g9s/internal/dao"
)

// entry holds a cached TableData snapshot and the time it expires.
type entry struct {
	data      *dao.TableData
	expiresAt time.Time
}

// key identifies a cache entry by resource type and GCP project.
type key struct {
	resource string
	project  string
}

// Cache is a thread-safe TTL store for dao.TableData snapshots.
type Cache struct {
	mu      sync.RWMutex
	entries map[key]entry
}

// New creates an empty Cache.
func New() *Cache {
	return &Cache{entries: make(map[key]entry)}
}

// Get returns the cached TableData for (resource, project) if it exists and
// has not yet expired. Returns (nil, false) on a miss or expiry.
func (c *Cache) Get(resource, project string) (*dao.TableData, bool) {
	c.mu.RLock()
	e, ok := c.entries[key{resource, project}]
	c.mu.RUnlock()

	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.data, true
}

// Set stores data under (resource, project) with the given TTL.
func (c *Cache) Set(resource, project string, data *dao.TableData, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key{resource, project}] = entry{
		data:      data,
		expiresAt: time.Now().Add(ttl),
	}
}

// Invalidate removes the entry for (resource, project), forcing the next
// Get to miss and the next refresh to call the DAO.
func (c *Cache) Invalidate(resource, project string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key{resource, project})
}

// Purge removes all expired entries. Call periodically if memory pressure is
// a concern; not required for correctness.
func (c *Cache) Purge() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
}
