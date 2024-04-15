package groupcache

import (
	"sync/atomic"
	"time"

	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/maypok86/otter"
)

type NowFunc func() time.Time

// OtterCache is an alternative cache implementation which uses a high performance lockless
// cache suitable for use in high concurrency environments where mutex contention is an issue.
type OtterCache struct {
	cache    otter.Cache[string, transport.ByteView]
	rejected atomic.Int64
	gets     atomic.Int64

	// Now is the Now() function the cache will use to determine
	// the current time which is used to calculate expired values
	// Defaults to time.Now()
	Now NowFunc
}

// NewOtterCache instantiates a new cache instance
//
// Due to the algorithm otter uses to evict and track cache item costs, it is recommended to
// use a larger maximum byte size when creating Groups via Instance.NewGroup() when using
// OtterCache if you expect your cached items to be very large. This is because groupcache
// uses a "Main Cache" and a "Hot Cache" system where the "Hot Cache" is 1/8th the size of
// the maximum bytes requested. Because Otter cache may reject items added to the cache
// which are larger than 1/10th of the total capacity of the "Hot Cache" this may result in
// a lower hit rate for the "Hot Cache" when storing large cache items and penalize the
// efficiency of groupcache operation.
//
// For Example:
// If you expect the average item in cache to be 100 bytes, and you create a Group with a cache size
// of 100,000 bytes, then the main cache will be 87,500 bytes and the hot cache will be 12,500 bytes.
// Since the largest possible item in otter cache is 1/10th of the total size of the cache. Then the
// largest item that could possibly fit into the hot cache is 1,250 bytes. If you think any of the
// items you store in groupcache could be larger than 1,250 bytes. Then you should increase the maximum
// bytes in a Group to accommodate the maximum cache item. If you have no estimate of the maximum size
// of items in the groupcache, then you should monitor the `Cache.Stats().Rejected` stat for the cache
// in production and adjust the size accordingly.
func NewOtterCache(maxBytes int64) (*OtterCache, error) {
	o := &OtterCache{
		Now: time.Now,
	}

	var err error
	o.cache, err = otter.MustBuilder[string, transport.ByteView](int(maxBytes)).
		CollectStats().
		Cost(func(key string, value transport.ByteView) uint32 {
			return uint32(value.Len())
		}).
		Build()
	return o, err
}

// Get returns the item from the cache
func (o *OtterCache) Get(key string) (transport.ByteView, bool) {
	i, ok := o.cache.Get(key)

	// We don't use otter's TTL as it is universal to every item
	// in the cache and groupcache allows users to set a TTL per
	// item stored.
	if !i.Expire().IsZero() && i.Expire().Before(o.Now()) {
		o.cache.Delete(key)
		return transport.ByteView{}, false
	}
	o.gets.Add(1)
	return i, ok
}

// Add adds the item to the cache. However, otter has the side effect
// of rejecting an item if the items size (aka, cost) is larger than
// the capacity (max cost) of the cache divided by 10.
//
// If Stats() reports a high number of Rejected items due to large
// cached items exceeding the maximum cost of the "Hot Cache", then you
// should increase the size of the cache such that no cache item is
// larger than the total size of the cache divided by 10.
//
// See s3fifo/policy.go NewPolicy() for details
func (o *OtterCache) Add(key string, value transport.ByteView) {
	if ok := o.cache.Set(key, value); !ok {
		o.rejected.Add(1)
	}
}

func (o *OtterCache) Remove(key string) {
	o.cache.Delete(key)
}

func (o *OtterCache) Stats() CacheStats {
	s := o.cache.Stats()
	return CacheStats{
		Bytes:     int64(o.cache.Capacity()),
		Items:     int64(o.cache.Size()),
		Rejected:  o.rejected.Load(),
		Evictions: s.EvictedCount(),
		Gets:      o.gets.Load(),
		Hits:      s.Hits(),
	}
}

// Bytes always returns 0 bytes used. Otter does not keep track of total bytes,
// and it is impractical for us to attempt to keep track of total bytes in the
// cache. Tracking the size of Add and Eviction is easy. However, we must also
// adjust the total bytes count when items with the same key are replaced.
// Doing so is more computationally expensive as we must check the cache for an
// existing item, subtract the existing byte count, then add the new byte count
// of the replacing item.
//
// Arguably reporting the total bytes used is not as useful as hit ratio
// in a production environment.
func (o *OtterCache) Bytes() int64 {
	return 0
}

func (o *OtterCache) Close() {
	o.cache.Close()
}
