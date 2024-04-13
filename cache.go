/*
Copyright 2012 Google Inc.
Copyright Derrick J Wippler

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

package groupcache

import (
	"sync"
	"time"

	"github.com/groupcache/groupcache-go/v3/internal/lru"
	"github.com/groupcache/groupcache-go/v3/transport"
)

// Cache is the interface a cache should implement
type Cache interface {
	// Get returns a ByteView from the cache
	Get(string) (transport.ByteView, bool)
	// Add adds a ByteView to the cache
	Add(string, transport.ByteView)
	// Stats returns stats about the current cache
	Stats() CacheStats
	// Remove removes a ByteView from the cache
	Remove(string)
	// Bytes returns the total number of bytes in the cache
	Bytes() int64
	// Close closes the cache. The implementation should shut down any
	// background operations.
	Close()
}

// nowFunc returns the current time which is used by the LRU to
// determine if the value has expired. This can be overridden by
// tests to ensure items are evicted when expired.
var nowFunc lru.NowFunc = time.Now

// mutexCache is a wrapper around an *lru.Cache that uses a mutex for
// synchronization, makes values always be ByteView, counts the size
// of all keys and values and automatically evicts them to keep the
// cache under the requested bytes limit.
type mutexCache struct {
	mu                    sync.RWMutex
	lru                   *lru.Cache
	bytes                 int64 // total bytes of all keys and values
	hits, gets, evictions int64
	maxBytes              int64
}

// newMutexCache creates a new cache. If maxBytes == 0 then size of the cache is unbounded.
func newMutexCache(maxBytes int64) *mutexCache {
	return &mutexCache{
		maxBytes: maxBytes,
	}
}

func (m *mutexCache) Stats() CacheStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return CacheStats{
		Bytes:     m.bytes,
		Items:     m.itemsLocked(),
		Gets:      m.gets,
		Hits:      m.hits,
		Evictions: m.evictions,
	}
}

func (m *mutexCache) Add(key string, value transport.ByteView) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lru == nil {
		m.lru = &lru.Cache{
			Now: nowFunc,
			OnEvicted: func(key lru.Key, value interface{}) {
				val := value.(transport.ByteView)
				m.bytes -= int64(len(key.(string))) + int64(val.Len())
				m.evictions++
			},
		}
	}
	m.lru.Add(key, value, value.Expire())
	m.bytes += int64(len(key)) + int64(value.Len())
	m.removeOldest()
}

func (m *mutexCache) Get(key string) (value transport.ByteView, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.lru == nil {
		return
	}
	vi, ok := m.lru.Get(key)
	if !ok {
		return
	}
	m.hits++
	return vi.(transport.ByteView), true
}

func (m *mutexCache) Remove(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lru == nil {
		return
	}
	m.lru.Remove(key)
}

func (m *mutexCache) Bytes() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bytes
}

func (m *mutexCache) Close() {
	// Do nothing
}

// removeOldest removes the oldest items in the cache until the number of bytes is
// under the maxBytes limit. Must be called from a function which already maintains
// a lock.
func (m *mutexCache) removeOldest() {
	if m.maxBytes == 0 {
		return
	}
	for {
		if m.bytes <= m.maxBytes {
			return
		}
		if m.lru != nil {
			m.lru.RemoveOldest()
		}
	}
}

func (m *mutexCache) itemsLocked() int64 {
	if m.lru == nil {
		return 0
	}
	return int64(m.lru.Len())
}
