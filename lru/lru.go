/*
Copyright 2013 Google Inc.

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

// Package lru implements an LRU cache.
package lru

import (
	"container/list"
	"time"
)

type NowFunc func() time.Time

// Cache is an LRU cache. It is not safe for concurrent access.
type Cache struct {
	// MaxEntries is the maximum number of cache entries before
	// an item is evicted. Zero means no limit.
	MaxEntries int

	// OnEvicted optionally specifies a callback function to be
	// executed when an entry is purged from the cache.
	OnEvicted func(key Key, value interface{}, nonExpiredAndMemFull bool)

	// Now is the Now() function the cache will use to determine
	// the current time which is used to calculate expired values
	// Defaults to time.Now()
	Now NowFunc

	// PurgeExpired enables removing all expired keys when the
	// oldest item is removed.
	PurgeExpired bool

	ll    *list.List
	cache map[interface{}]*list.Element
}

// A Key may be any value that is comparable. See http://golang.org/ref/spec#Comparison_operators
type Key interface{}

type entry struct {
	key    Key
	value  interface{}
	expire time.Time
}

// New creates a new Cache.
// If maxEntries is zero, the cache has no limit and it's assumed
// that eviction is done by the caller.
func New(maxEntries int) *Cache {
	return &Cache{
		MaxEntries: maxEntries,
		ll:         list.New(),
		cache:      make(map[interface{}]*list.Element),
		Now:        time.Now,
	}
}

// Add adds a value to the cache.
func (c *Cache) Add(key Key, value interface{}, expire time.Time) {
	if c.cache == nil {
		c.cache = make(map[interface{}]*list.Element)
		c.ll = list.New()
	}
	if ee, ok := c.cache[key]; ok {
		eee := ee.Value.(*entry)
		if c.OnEvicted != nil {
			const nonExpiredAndMemFull = false // not a mem full condition
			c.OnEvicted(key, eee.value, nonExpiredAndMemFull)
		}
		c.ll.MoveToFront(ee)
		eee.expire = expire
		eee.value = value
		return
	}
	ele := c.ll.PushFront(&entry{key, value, expire})
	c.cache[key] = ele
	if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
		c.RemoveOldest() // RemoveOldest is used on mem full condition.
	}
}

// Get looks up a key's value from the cache.
func (c *Cache) Get(key Key) (value interface{}, ok bool) {
	if c.cache == nil {
		return
	}
	if ele, hit := c.cache[key]; hit {
		entry := ele.Value.(*entry)
		// If the entry has expired, remove it from the cache
		if expired := hasExpired(entry, c.Now()); expired {
			const memFull = false
			c.removeElement(ele, memFull, time.Now())
			return nil, false
		}

		c.ll.MoveToFront(ele)
		return entry.value, true
	}
	return
}

func hasExpired(entry *entry, now time.Time) bool {
	return !entry.expire.IsZero() && entry.expire.Before(now)
}

// Remove removes the provided key from the cache.
func (c *Cache) Remove(key Key) {
	if c.cache == nil {
		return
	}
	if ele, hit := c.cache[key]; hit {
		const memFull = false // memFull is used in stats for evictions
		c.removeElement(ele, memFull, time.Now())
	}
}

// RemoveOldest removes the oldest item from the cache.
// RemoveOldest is used on mem full condition.
func (c *Cache) RemoveOldest() {
	if c.cache == nil {
		return
	}

	// remove oldest item
	ele := c.ll.Back()
	if ele == nil {
		return
	}
	const memFull = true
	c.removeElement(ele, memFull, time.Now())
}

// RemoveAllExpired removes all expired items from the cache.
// RemoveAllExpired is used on mem full condition.
func (c *Cache) RemoveAllExpired() {
	if c.cache == nil {
		return
	}
	if !c.PurgeExpired {
		return
	}
	now := c.Now()
	for {
		ele := c.ll.Back()
		if ele == nil {
			break
		}
		entry := ele.Value.(*entry)
		if expired := hasExpired(entry, now); !expired {
			break
		}
		const memFull = true
		c.removeElement(ele, memFull, now)
	}
}

func (c *Cache) removeElement(e *list.Element, memFull bool, now time.Time) {
	c.ll.Remove(e)
	kv := e.Value.(*entry)
	delete(c.cache, kv.key)
	if c.OnEvicted != nil {
		nonExpiredAndMemFull := memFull && !hasExpired(kv, now)
		c.OnEvicted(kv.key, kv.value, nonExpiredAndMemFull)
	}
}

// Len returns the number of items in the cache.
func (c *Cache) Len() int {
	if c.cache == nil {
		return 0
	}
	return c.ll.Len()
}

// Clear purges all stored items from the cache.
func (c *Cache) Clear() {
	if c.OnEvicted != nil {
		for _, e := range c.cache {
			kv := e.Value.(*entry)
			const nonExpiredAndMemFull = false // not a mem full condition
			c.OnEvicted(kv.key, kv.value, nonExpiredAndMemFull)
		}
	}
	c.ll = nil
	c.cache = nil
}
