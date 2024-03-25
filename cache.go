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

	"github.com/groupcache/groupcache-go/v3/data"
	"github.com/groupcache/groupcache-go/v3/internal/lru"
)

// nowFunc returns the current time which is used by the LRU to
// determine if the value has expired. This can be overridden by
// tests to ensure items are evicted when expired.
var nowFunc lru.NowFunc = time.Now

// cache is a wrapper around an *lru.Cache that adds synchronization,
// makes values always be ByteView, and counts the size of all keys and
// values.
type cache struct {
	mu         sync.RWMutex
	nbytes     int64 // of all keys and values
	lru        *lru.Cache
	nhit, nget int64
	nevict     int64 // number of evictions
}

func (c *cache) stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CacheStats{
		Bytes:     c.nbytes,
		Items:     c.itemsLocked(),
		Gets:      c.nget,
		Hits:      c.nhit,
		Evictions: c.nevict,
	}
}

func (c *cache) add(key string, value data.ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		c.lru = &lru.Cache{
			Now: nowFunc,
			OnEvicted: func(key lru.Key, value interface{}) {
				val := value.(data.ByteView)
				c.nbytes -= int64(len(key.(string))) + int64(val.Len())
				c.nevict++
			},
		}
	}
	c.lru.Add(key, value, value.Expire())
	c.nbytes += int64(len(key)) + int64(value.Len())
}

func (c *cache) get(key string) (value data.ByteView, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nget++
	if c.lru == nil {
		return
	}
	vi, ok := c.lru.Get(key)
	if !ok {
		return
	}
	c.nhit++
	return vi.(data.ByteView), true
}

func (c *cache) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		return
	}
	c.lru.Remove(key)
}

func (c *cache) removeOldest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru != nil {
		c.lru.RemoveOldest()
	}
}

func (c *cache) bytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nbytes
}

func (c *cache) itemsLocked() int64 {
	if c.lru == nil {
		return 0
	}
	return int64(c.lru.Len())
}
