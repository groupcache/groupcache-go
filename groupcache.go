/*
Copyright 2012 Google Inc.

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

// Package groupcache provides a data loading mechanism with caching
// and de-duplication that works across a set of peer processes.
//
// Each data Get first consults its local cache, otherwise delegates
// to the requested key's canonical owner, which then checks its cache
// or finally gets the data.  In the common case, many concurrent
// cache misses across a set of peers for the same key result in just
// one cache fill.
package groupcache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	pb "github.com/modernprogram/groupcache/v2/groupcachepb"
	"github.com/modernprogram/groupcache/v2/lru"
	"github.com/modernprogram/groupcache/v2/singleflight"
)

// A Getter loads data for a key.
type Getter interface {
	// Get returns the value identified by key, populating dest.
	//
	// The returned data must be unversioned. That is, key must
	// uniquely describe the loaded data, without an implicit
	// current time, and without relying on cache expiration
	// mechanisms.
	Get(ctx context.Context, key string, dest Sink, info *Info) error
}

// A GetterFunc implements Getter with a function.
type GetterFunc func(ctx context.Context, key string, dest Sink, info *Info) error

func (f GetterFunc) Get(ctx context.Context, key string, dest Sink, info *Info) error {
	return f(ctx, key, dest, info)
}

// GetGroupWithWorkspace returns the named group previously created with NewGroup, or
// nil if there's no such group.
func GetGroupWithWorkspace(ws *Workspace, name string) *Group {
	ws.mu.RLock()
	g := ws.groups[name]
	ws.mu.RUnlock()
	return g
}

// Options define settings for group.
type Options struct {
	Workspace       *Workspace
	Name            string
	CacheBytesLimit int64
	Getter          Getter

	// HotCacheWeight and MainCacheWeight compose the ratio between the hot cache and the main cache.
	// When the cache memory limit is reached, this ratio is used to limit each cache to its limit.
	// One simple way to reason about this ratio is to think of one cache usage against
	// the other, and NOT one cache against the total sum.
	//
	// Consider some examples:
	// 1) mainWeigth=8 and hotWeight=1 => the hot cache limit is 1/8 of the main cache limit.
	// 2) mainWeight=2 and hotWeight=1 => the hot cache limit is 1/2 of the main cache limit.
	// 3) mainWeight=1 and hotWeight=1 => the hot cache limit is equal to the main cache limit.
	// 4) mainWeight=1 and hotWeight=2 => the hot cache limit is 2x the main cache limit.
	// 5) mainWeight=1 and hotWeight=8 => the hot cache limit is 8x the main cache limit.
	//
	// If unspecified, HotCacheWeight defaults to 1.
	// If unspecified, MainCacheWeight defaults to 8.
	HotCacheWeight  int64
	MainCacheWeight int64

	// PurgeExpired enables evicting expired keys on memory full condition.
	PurgeExpired bool

	// ExpiredKeysEvictionInterval sets interval for periodic eviction of expired keys.
	// If unset, defaults to 30-minute period.
	// Set to -1 to disable periodic eviction of expired keys.
	ExpiredKeysEvictionInterval time.Duration

	// Logger is optional pluggable logger.
	// If undefined, groupcache won't log anything.
	// If defined, groupcache will log errors retrieving keys from peers.
	// slog.Defaut() creates a logger that satifiest this interface.
	Logger Logger
}

// Logger is interface for pluggable logger.
// slog.Defaut() creates a logger that satifiest this interface.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewGroupWithWorkspace creates a coordinated group-aware Getter from a Getter.
//
// The returned Getter tries (but does not guarantee) to run only one
// Get call at once for a given key across an entire set of peer
// processes. Concurrent callers both in the local process and in
// other processes receive copies of the answer once the original Get
// completes.
//
// The group name must be unique for each getter.
func NewGroupWithWorkspace(options Options) *Group {

	if options.MainCacheWeight == 0 {
		options.MainCacheWeight = 8
	}

	if options.HotCacheWeight == 0 {
		options.HotCacheWeight = 1
	}

	if options.ExpiredKeysEvictionInterval == 0 {
		options.ExpiredKeysEvictionInterval = 30 * time.Minute
	}

	return newGroup(options.Workspace, options.Name, options.PurgeExpired,
		options.CacheBytesLimit, options.MainCacheWeight, options.HotCacheWeight,
		options.ExpiredKeysEvictionInterval, options.Getter, nil, options.Logger)
}

// If peers is nil, the peerPicker is called via a sync.Once to initialize it.
func newGroup(ws *Workspace, name string, purgeExpired bool, cacheBytesLimit,
	mainCacheWeight, hotCacheWeight int64,
	expiredKeysEvictionInterval time.Duration, getter Getter,
	peers PeerPicker, logger Logger) *Group {
	if getter == nil {
		panic("nil Getter")
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if _, dup := ws.groups[name]; dup {
		panic("duplicate registration of group " + name)
	}
	g := &Group{
		ws:              ws,
		logger:          logger,
		name:            name,
		getter:          getter,
		peers:           peers,
		cacheBytesLimit: cacheBytesLimit,
		mainCacheWeight: mainCacheWeight,
		hotCacheWeight:  hotCacheWeight,
		purgeExpired:    purgeExpired,
		loadGroup:       &singleflight.Group{},
		setGroup:        &singleflight.Group{},
		removeGroup:     &singleflight.Group{},
	}
	ws.groups[name] = g

	if expiredKeysEvictionInterval > 0 {
		// launch goroutine to periodically evict expired keys
		weakGroup := weak.Make(g)
		go evictExpiredKeys(weakGroup, expiredKeysEvictionInterval)
	}

	return g
}

// evictExpiredKeys periodically evicts all expired keys.
func evictExpiredKeys(weakGroup weak.Pointer[Group],
	expiredKeysEvictionInterval time.Duration) {
	c := time.Tick(expiredKeysEvictionInterval)
	for range c {
		strong := weakGroup.Value()
		if strong == nil {
			// group has been garbage collected,
			// exit to release goroutine resources
			return
		}
		strong.removeAllExpired()
		strong = nil // make sure we got rid of the strong pointer
	}
}

// A Group is a cache namespace and associated data loaded spread over
// a group of 1 or more machines.
type Group struct {
	ws              *Workspace
	logger          Logger
	name            string
	getter          Getter
	peersOnce       sync.Once
	peers           PeerPicker
	cacheBytesLimit int64 // limit for sum of mainCache and hotCache size

	mainCacheWeight int64
	hotCacheWeight  int64
	purgeExpired    bool

	// mainCache is a cache of the keys for which this process
	// (amongst its peers) is authoritative. That is, this cache
	// contains keys which consistent hash on to this process's
	// peer number.
	mainCache cache

	// hotCache contains keys/values for which this peer is not
	// authoritative (otherwise they would be in mainCache), but
	// are popular enough to warrant mirroring in this process to
	// avoid going over the network to fetch from a peer.  Having
	// a hotCache avoids network hotspotting, where a peer's
	// network card could become the bottleneck on a popular key.
	// This cache is used sparingly to maximize the total number
	// of key/value pairs that can be stored globally.
	hotCache cache

	// loadGroup ensures that each key is only fetched once
	// (either locally or remotely), regardless of the number of
	// concurrent callers.
	loadGroup flightGroup

	// setGroup ensures that each added key is only added
	// remotely once regardless of the number of concurrent callers.
	setGroup flightGroup

	// removeGroup ensures that each removed key is only removed
	// remotely once regardless of the number of concurrent callers.
	removeGroup flightGroup

	_ int32 // force Stats to be 8-byte aligned on 32-bit platforms

	// Stats are statistics on the group.
	Stats Stats
}

// flightGroup is defined as an interface which flightgroup.Group
// satisfies.  We define this so that we may test with an alternate
// implementation.
type flightGroup interface {
	Do(key string, fn func() (interface{}, error)) (interface{}, error)
	Lock(fn func())
}

// Stats are per-group statistics.
type Stats struct {
	Gets                     AtomicInt // any Get request, including from peers
	CacheHits                AtomicInt // either cache was good
	GetFromPeersLatencyLower AtomicInt // slowest duration to request value from peers
	PeerLoads                AtomicInt // either remote load or remote cache hit (not an error)
	PeerErrors               AtomicInt
	Loads                    AtomicInt // (gets - cacheHits)
	LoadsDeduped             AtomicInt // after singleflight
	LocalLoads               AtomicInt // total good local loads
	LocalLoadErrs            AtomicInt // total bad local loads
	ServerRequests           AtomicInt // gets that came over the network from peers
	CrosstalkRefusals        AtomicInt // refusals for additional crosstalks
}

// Name returns the name of the group.
func (g *Group) Name() string {
	return g.name
}

func (g *Group) initPeers() {
	if g.peers == nil {
		g.peers = getPeers(g.ws, g.name)
	}
}

// Get retrieves key for library caller, thus crosstalk is allowed.
// info holds optional user-supplied per-request context fields that are
// propagated to the peer getter load function.
func (g *Group) Get(ctx context.Context, key string, dest Sink, info *Info) error {
	const crosstalkAllowed = true
	return g.get(ctx, key, dest, crosstalkAllowed, info)
}

// GetForPeer retrieves key for peer in a crosstalk request, thus further crosstalk won't be allowed.
func (g *Group) GetForPeer(ctx context.Context, key string, dest Sink, info *Info) error {
	const crosstalkAllowed = false
	return g.get(ctx, key, dest, crosstalkAllowed, info)
}

func (g *Group) get(ctx context.Context, key string, dest Sink,
	crosstalkAllowed bool, info *Info) error {
	g.peersOnce.Do(g.initPeers)
	g.Stats.Gets.Add(1)
	if dest == nil {
		return errors.New("groupcache: nil dest Sink")
	}
	value, cacheHit := g.lookupCache(key)

	if cacheHit {
		g.Stats.CacheHits.Add(1)
		return setSinkView(dest, value)
	}

	// Optimization to avoid double unmarshalling or copying: keep
	// track of whether the dest was already populated. One caller
	// (if local) will set this; the losers will not. The common
	// case will likely be one caller.
	value, destPopulated, err := g.load(ctx, key, dest, crosstalkAllowed, info)
	if err != nil {
		return err
	}
	if destPopulated {
		return nil
	}
	return setSinkView(dest, value)
}

func (g *Group) Set(ctx context.Context, key string, value []byte, expire time.Time, hotCache bool) error {
	g.peersOnce.Do(g.initPeers)

	if key == "" {
		return errors.New("empty Set() key not allowed")
	}

	_, err := g.setGroup.Do(key, func() (interface{}, error) {
		// If remote peer owns this key
		owner, ok := g.peers.PickPeer(key)
		if ok {
			if err := g.setFromPeer(ctx, owner, key, value, expire); err != nil {
				return nil, err
			}
			// TODO(thrawn01): Not sure if this is useful outside of tests...
			//  maybe we should ALWAYS update the local cache?
			if hotCache {
				g.localSet(key, value, expire, &g.hotCache)
			}
			return nil, nil
		}
		// We own this key
		g.localSet(key, value, expire, &g.mainCache)
		return nil, nil
	})
	return err
}

// Remove clears the key from our cache then forwards the remove
// request to all peers.
func (g *Group) Remove(ctx context.Context, key string) error {
	g.peersOnce.Do(g.initPeers)

	_, err := g.removeGroup.Do(key, func() (interface{}, error) {

		// Remove from key owner first
		owner, ok := g.peers.PickPeer(key)
		if ok {
			if err := g.removeFromPeer(ctx, owner, key); err != nil {
				return nil, err
			}
		}
		// Remove from our cache next
		g.localRemove(key)
		wg := sync.WaitGroup{}
		errs := make(chan error)

		// Asynchronously clear the key from all hot and main caches of peers
		for _, peer := range g.peers.GetAll() {
			// avoid deleting from owner a second time
			if peer == owner {
				continue
			}

			wg.Add(1)
			go func(peer ProtoGetter) {
				errs <- g.removeFromPeer(ctx, peer, key)
				wg.Done()
			}(peer)
		}
		go func() {
			wg.Wait()
			close(errs)
		}()

		// TODO(thrawn01): Should we report all errors? Reporting context
		//  cancelled error for each peer doesn't make much sense.
		var err error
		for e := range errs {
			err = e
		}

		return nil, err
	})
	return err
}

var errFurtherCrosstalkRefused = errors.New("further crosstalk refused")

// load loads key either by invoking the getter locally or by sending it to another machine.
func (g *Group) load(ctx context.Context, key string, dest Sink,
	crosstalkAllowed bool, info *Info) (value ByteView, destPopulated bool, err error) {
	g.Stats.Loads.Add(1)
	viewi, err := g.loadGroup.Do(key, func() (interface{}, error) {
		// Check the cache again because singleflight can only dedup calls
		// that overlap concurrently.  It's possible for 2 concurrent
		// requests to miss the cache, resulting in 2 load() calls.  An
		// unfortunate goroutine scheduling would result in this callback
		// being run twice, serially.  If we don't check the cache again,
		// cache.nbytes would be incremented below even though there will
		// be only one entry for this key.
		//
		// Consider the following serialized event ordering for two
		// goroutines in which this callback gets called twice for hte
		// same key:
		// 1: Get("key")
		// 2: Get("key")
		// 1: lookupCache("key")
		// 2: lookupCache("key")
		// 1: load("key")
		// 2: load("key")
		// 1: loadGroup.Do("key", fn)
		// 1: fn()
		// 2: loadGroup.Do("key", fn)
		// 2: fn()
		if value, cacheHit := g.lookupCache(key); cacheHit {
			g.Stats.CacheHits.Add(1)
			return value, nil
		}
		g.Stats.LoadsDeduped.Add(1)
		var value ByteView
		var err error
		if peer, ok := g.peers.PickPeer(key); ok {

			// other peer (not me) is the key owner

			if !crosstalkAllowed {
				// crosstalkAllowed=false: we are responding to a crosstalk request.
				// then we refuse further crosstalk.
				g.Stats.CrosstalkRefusals.Add(1)
				return nil, errFurtherCrosstalkRefused
			}

			// metrics duration start
			start := time.Now()

			// get value from peers
			value, err = g.getFromPeer(ctx, peer, key, info)

			// metrics duration compute
			duration := int64(time.Since(start)) / int64(time.Millisecond)

			// metrics only store the slowest duration
			if g.Stats.GetFromPeersLatencyLower.Get() < duration {
				g.Stats.GetFromPeersLatencyLower.Store(duration)
			}

			if err == nil {
				g.Stats.PeerLoads.Add(1)
				return value, nil
			}

			if errors.Is(err, context.Canceled) {
				return nil, err
			}

			if errors.Is(err, &ErrNotFound{}) {
				return nil, err
			}

			if errors.Is(err, &ErrRemoteCall{}) {
				return nil, err
			}

			if g.logger != nil {
				g.logger.Error(fmt.Sprintf("error retrieving key from peer '%s'", peer.GetURL()),
					"err", err,
					"key", key,
					"category", "groupcache",
				)
			}

			g.Stats.PeerErrors.Add(1)
			if ctx != nil && ctx.Err() != nil {
				// Return here without attempting to get locally
				// since the context is no longer valid
				return nil, err
			}

			// the crosstalk peer request above failed with an unexpected error
			// FIXME TODO XXX: we are currently generating the key by ourselves, but should we?
		}

		// either
		//   1. we are the key owner
		//      then we will generate the key by ourselves
		// or
		//   2. the crosstalk peer request above failed with an unexpected error
		//      FIXME TODO XXX: we are currently generating the key by ourselves, but should we?

		value, err = g.getLocally(ctx, key, dest, info)
		if err != nil {
			g.Stats.LocalLoadErrs.Add(1)
			return nil, err
		}
		g.Stats.LocalLoads.Add(1)
		destPopulated = true // only one caller of load gets this return value
		g.populateCache(key, value, &g.mainCache)
		return value, nil
	})
	if err == nil {
		value = viewi.(ByteView)
	}
	return
}

func (g *Group) getLocally(ctx context.Context, key string, dest Sink, info *Info) (ByteView, error) {
	err := g.getter.Get(ctx, key, dest, info)
	if err != nil {
		return ByteView{}, err
	}
	return dest.view()
}

func (g *Group) getFromPeer(ctx context.Context, peer ProtoGetter,
	key string, info *Info) (ByteView, error) {

	req := &pb.GetRequest{
		Group: &g.name,
		Key:   &key,
	}

	if info != nil {
		// Propagate optional user-supplied per-request information to peer load function.
		req.Ctx1 = &info.Ctx1
		req.Ctx2 = &info.Ctx2
	}

	res := &pb.GetResponse{}
	err := peer.Get(ctx, req, res)
	if err != nil {
		return ByteView{}, err
	}

	var expire time.Time
	if res.Expire != nil && *res.Expire != 0 {
		expire = time.Unix(*res.Expire/int64(time.Second), *res.Expire%int64(time.Second))
		if time.Now().After(expire) {
			return ByteView{}, errors.New("peer returned expired value")
		}
	}

	value := ByteView{b: res.Value, e: expire}

	// Always populate the hot cache
	g.populateCache(key, value, &g.hotCache)
	return value, nil
}

func (g *Group) setFromPeer(ctx context.Context, peer ProtoGetter, k string, v []byte, e time.Time) error {
	var expire int64
	if !e.IsZero() {
		expire = e.UnixNano()
	}
	req := &pb.SetRequest{
		Expire: &expire,
		Group:  &g.name,
		Key:    &k,
		Value:  v,
	}
	return peer.Set(ctx, req)
}

func (g *Group) removeFromPeer(ctx context.Context, peer ProtoGetter, key string) error {
	req := &pb.GetRequest{
		Group: &g.name,
		Key:   &key,
	}
	return peer.Remove(ctx, req)
}

func (g *Group) lookupCache(key string) (value ByteView, ok bool) {
	if g.cacheBytesLimit <= 0 {
		return
	}
	value, ok = g.mainCache.get(key)
	if ok {
		return
	}
	value, ok = g.hotCache.get(key)
	return
}

func (g *Group) localSet(key string, value []byte, expire time.Time, cache *cache) {
	if g.cacheBytesLimit <= 0 {
		return
	}

	bv := ByteView{
		b: value,
		e: expire,
	}

	// Ensure no requests are in flight
	g.loadGroup.Lock(func() {
		g.populateCache(key, bv, cache)
	})
}

func (g *Group) localRemove(key string) {
	// Clear key from our local cache
	if g.cacheBytesLimit <= 0 {
		return
	}

	// Ensure no requests are in flight
	g.loadGroup.Lock(func() {
		g.hotCache.remove(key)
		g.mainCache.remove(key)
	})
}

func (g *Group) removeAllExpired() int64 {
	mainBytes := g.mainCache.removeAllExpired()
	hotBytes := g.hotCache.removeAllExpired()
	return mainBytes + hotBytes
}

func (g *Group) populateCache(key string, value ByteView, c *cache) {
	if g.cacheBytesLimit <= 0 {
		return
	}
	c.add(key, value)

	if g.purgeExpired {
		{
			mainBytes := g.mainCache.bytes()
			hotBytes := g.hotCache.bytes()
			if mainBytes+hotBytes <= g.cacheBytesLimit {
				return // mem is not full
			}
		}

		// first, attempt to evict only expired keys in order to prevent
		// evicting non-expired keys.
		if mainAndHotBytes := g.removeAllExpired(); mainAndHotBytes <= g.cacheBytesLimit {
			return // mem no longer full
		}
	}

	// mem is still full.
	// now we will evict oldest non-expired keys.

	// Evict items from cache(s) if necessary.
	for {
		mainBytes := g.mainCache.bytes()
		hotBytes := g.hotCache.bytes()
		if mainBytes+hotBytes <= g.cacheBytesLimit {
			return // mem no longer full
		}

		// here we are on mem full condition.

		// TODO(bradfitz): this is good-enough-for-now logic.
		// It should be something based on measurements and/or
		// respecting the costs of different resources.
		var victim *cache
		// default weights: if hotBytes > mainBytes/8 { ... }
		if isHotUsageExcessive(mainBytes, hotBytes, g.mainCacheWeight, g.hotCacheWeight) {
			victim = &g.hotCache
		} else {
			victim = &g.mainCache
		}

		victim.removeOldest() // removeOldest is used on mem full condition.
	}
}

// isHotUsageExcessive reports if hot cache exceeded its limit in comparisson to main cache.
// Conversely, it also reports if main cache did NOT exceed its limit in comparisson to hot cache.
// For the special case when both caches are using their exact limits, we report the hot cache
// is exceeding its limit, thus favouring the main cache slightly.
// One simple way to reason about this function behavior is to think of one cache usage against
// the other, and NOT one cache against the total sum.
// Consider some examples:
// 1) mainWeigth=8 and hotWeight=1 => the hot cache limit is 1/8 of the main cache limit.
// 2) mainWeight=2 and hotWeight=1 => the hot cache limit is 1/2 of the main cache limit.
// 3) mainWeight=1 and hotWeight=1 => the hot cache limit is equal to the main cache limit.
// 4) mainWeight=1 and hotWeight=2 => the hot cache limit is 2x the main cache limit.
// 5) mainWeight=1 and hotWeight=8 => the hot cache limit is 8x the main cache limit.
func isHotUsageExcessive(mainBytes, hotBytes, mainWeight, hotWeight int64) bool {
	return hotBytes*mainWeight > mainBytes*hotWeight
}

// CacheType represents a type of cache.
type CacheType int

const (
	// MainCache is the cache for items that this peer is the
	// owner for.
	MainCache CacheType = iota + 1

	// HotCache is the cache for items that seem popular
	// enough to replicate to this node, even though it's not the
	// owner.
	HotCache
)

// CacheStats returns stats about the provided cache within the group.
func (g *Group) CacheStats(which CacheType) CacheStats {
	switch which {
	case MainCache:
		return g.mainCache.stats()
	case HotCache:
		return g.hotCache.stats()
	default:
		return CacheStats{}
	}
}

// NowFunc returns the current time which is used by the LRU to
// determine if the value has expired. This can be overridden by
// tests to ensure items are evicted when expired.
var NowFunc lru.NowFunc = time.Now

// cache is a wrapper around an *lru.Cache that adds synchronization,
// makes values always be ByteView, and counts the size of all keys and
// values.
type cache struct {
	mu                        sync.RWMutex
	nbytes                    int64 // of all keys and values
	lru                       *lru.Cache
	nhit, nget                int64
	nevict                    int64 // number of evictions
	nevictNonExpiredOnMemFull int64 // number of evictions for non-expired items on mem full condition
}

func (c *cache) stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CacheStats{
		Bytes:                        c.nbytes,
		Items:                        c.itemsLocked(),
		Gets:                         c.nget,
		Hits:                         c.nhit,
		Evictions:                    c.nevict,
		EvictionsNonExpiredOnMemFull: c.nevictNonExpiredOnMemFull,
	}
}

func (c *cache) add(key string, value ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		c.lru = &lru.Cache{
			Now: NowFunc,
			OnEvicted: func(key lru.Key, value interface{}, nonExpiredAndMemFull bool) {
				val := value.(ByteView)
				c.nbytes -= int64(len(key.(string))) + int64(val.Len())
				c.nevict++
				if nonExpiredAndMemFull {
					c.nevictNonExpiredOnMemFull++
				}
			},
		}
	}
	c.lru.Add(key, value, value.Expire())
	c.nbytes += int64(len(key)) + int64(value.Len())
}

func (c *cache) get(key string) (value ByteView, ok bool) {
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
	return vi.(ByteView), true
}

func (c *cache) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		return
	}
	c.lru.Remove(key)
}

// removeOldest is used on mem full condition.
func (c *cache) removeOldest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru != nil {
		c.lru.RemoveOldest()
	}
}

// removeAllExpired is used on mem full condition.
func (c *cache) removeAllExpired() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru != nil {
		c.lru.RemoveAllExpired()
	}
	return c.nbytes
}

func (c *cache) bytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nbytes
}

func (c *cache) items() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.itemsLocked()
}

func (c *cache) itemsLocked() int64 {
	if c.lru == nil {
		return 0
	}
	return int64(c.lru.Len())
}

// An AtomicInt is an int64 to be accessed atomically.
type AtomicInt int64

// Add atomically adds n to i.
func (i *AtomicInt) Add(n int64) {
	atomic.AddInt64((*int64)(i), n)
}

// Store atomically stores n to i.
func (i *AtomicInt) Store(n int64) {
	atomic.StoreInt64((*int64)(i), n)
}

// Get atomically gets the value of i.
func (i *AtomicInt) Get() int64 {
	return atomic.LoadInt64((*int64)(i))
}

func (i *AtomicInt) String() string {
	return strconv.FormatInt(i.Get(), 10)
}

// CacheStats are returned by stats accessors on Group.
type CacheStats struct {
	Bytes                        int64
	Items                        int64
	Gets                         int64
	Hits                         int64
	Evictions                    int64
	EvictionsNonExpiredOnMemFull int64 // number of evictions for non-expired items on mem full condition
}
