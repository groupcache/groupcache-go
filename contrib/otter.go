package contrib

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maypok86/otter"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/transport"
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

var _ groupcache.Cache = (*OtterCache)(nil)

// NewOtterCache instantiates a new cache instance
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

func (o *OtterCache) Stats() groupcache.CacheStats {
	s := o.cache.Stats()
	return groupcache.CacheStats{
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

func (o *OtterCache) RemoveKeys(keys ...string) {
	if len(keys) == 0 {
		return
	}

	if len(keys) == 1 {
		o.cache.Delete(keys[0])
		return
	}

	workerCount := runtime.GOMAXPROCS(0)
	if workerCount > len(keys) {
		workerCount = len(keys)
	}

	// For very small batches, a simple loop is faster than spinning up workers.
	if workerCount <= 1 {
		for _, key := range keys {
			o.cache.Delete(key)
		}
		return
	}

	keyCh := make(chan string, workerCount)
	var wg sync.WaitGroup

	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for k := range keyCh {
				o.cache.Delete(k)
			}
		}()
	}

	for _, key := range keys {
		keyCh <- key
	}
	close(keyCh)
	wg.Wait()
}
