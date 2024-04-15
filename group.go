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
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/groupcache/groupcache-go/v3/internal/singleflight"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/pb"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

// Group is the user facing interface for a group
type Group interface {
	Set(context.Context, string, []byte, time.Time, bool) error
	Get(context.Context, string, transport.Sink) error
	Remove(context.Context, string) error
	UsedBytes() (int64, int64)
	Name() string
}

// A Getter loads data for a key.
type Getter interface {
	// Get returns the value identified by key, populating dest.
	//
	// The returned data must be unversioned. That is, key must
	// uniquely describe the loaded data, without an implicit
	// current time, and without relying on cache expiration
	// mechanisms.
	Get(ctx context.Context, key string, dest transport.Sink) error
}

// A GetterFunc implements Getter with a function.
type GetterFunc func(ctx context.Context, key string, dest transport.Sink) error

func (f GetterFunc) Get(ctx context.Context, key string, dest transport.Sink) error {
	return f(ctx, key, dest)
}

// A Group is a cache namespace and associated data loaded spread over
// a group of 1 or more machines.
type group struct {
	name       string
	getter     Getter
	instance   *Instance
	cacheBytes int64 // limit for sum of mainCache and hotCache size

	// mainCache is a cache of the keys for which this process
	// (amongst its peers) is authoritative. That is, this cache
	// contains keys which consistent hash on to this process's
	// peer number.
	mainCache Cache

	// hotCache contains keys/values for which this peer is not
	// authoritative (otherwise they would be in mainCache), but
	// are popular enough to warrant mirroring in this process to
	// avoid going over the network to fetch from a peer.  Having
	// a hotCache avoids network hot spotting, where a peer's
	// network card could become the bottleneck on a popular key.
	// This cache is used sparingly to maximize the total number
	// of key/value pairs that can be stored globally.
	hotCache Cache

	// loadGroup ensures that each key is only fetched once
	// (either locally or remotely), regardless of the number of
	// concurrent callers.
	loadGroup *singleflight.Group

	// setGroup ensures that each added key is only added
	// remotely once regardless of the number of concurrent callers.
	setGroup *singleflight.Group

	// removeGroup ensures that each removed key is only removed
	// remotely once regardless of the number of concurrent callers.
	removeGroup *singleflight.Group

	// Stats are statistics on the group.
	Stats GroupStats
}

// Name returns the name of the group.
func (g *group) Name() string {
	return g.name
}

// UsedBytes returns the total number of bytes used by the main and hot caches
func (g *group) UsedBytes() (mainCache int64, hotCache int64) {
	return g.mainCache.Bytes(), g.hotCache.Bytes()
}

func (g *group) Get(ctx context.Context, key string, dest transport.Sink) error {
	g.Stats.Gets.Add(1)
	if dest == nil {
		return errors.New("groupcache: nil dest Sink")
	}
	value, cacheHit := g.lookupCache(key)

	if cacheHit {
		g.Stats.CacheHits.Add(1)
		return transport.SetSinkView(dest, value)
	}

	// Optimization to avoid double unmarshalling or copying: keep
	// track of whether the dest was already populated. One caller
	// (if local) will set this; the losers will not. The common
	// case will likely be one caller.
	var destPopulated bool
	value, destPopulated, err := g.load(ctx, key, dest)
	if err != nil {
		return err
	}
	if destPopulated {
		return nil
	}
	return transport.SetSinkView(dest, value)
}

func (g *group) Set(ctx context.Context, key string, value []byte, expire time.Time, hotCache bool) error {
	if key == "" {
		return errors.New("empty Set() key not allowed")
	}

	_, err := g.setGroup.Do(key, func() (interface{}, error) {
		// If remote peer owns this key
		owner, isRemote := g.instance.PickPeer(key)
		if isRemote {
			if err := g.setFromPeer(ctx, owner, key, value, expire); err != nil {
				return nil, err
			}
			// TODO(thrawn01): Not sure if this is useful outside of tests...
			//  maybe we should ALWAYS update the local cache?
			if hotCache {
				g.localSet(key, value, expire, g.hotCache)
			}
			return nil, nil
		}
		// We own this key
		g.localSet(key, value, expire, g.mainCache)
		return nil, nil
	})
	return err
}

// Remove clears the key from our cache then forwards the remove
// request to all peers.
//
// ### Consistency Warning
// This method implements a best case design since it is possible a temporary network disruption could
// occur resulting in remove requests never making it their peers. In practice this scenario is rare
// and the system typically remains consistent. However, in case of an inconsistency we recommend placing
// an expiration time on your values to ensure the cluster eventually becomes consistent again.
func (g *group) Remove(ctx context.Context, key string) error {
	_, err := g.removeGroup.Do(key, func() (interface{}, error) {

		// Remove from key owner first
		owner, isRemote := g.instance.PickPeer(key)
		if isRemote {
			if err := g.removeFromPeer(ctx, owner, key); err != nil {
				return nil, err
			}
		}
		// Remove from our cache next
		g.LocalRemove(key)
		wg := sync.WaitGroup{}
		errCh := make(chan error)

		// Asynchronously clear the key from all hot and main caches of peers
		for _, p := range g.instance.getAllPeers() {
			// avoid deleting from owner a second time
			if p == owner {
				continue
			}

			wg.Add(1)
			go func(p peer.Client) {
				errCh <- g.removeFromPeer(ctx, p, key)
				wg.Done()
			}(p)
		}
		go func() {
			wg.Wait()
			close(errCh)
		}()

		m := &MultiError{}
		for err := range errCh {
			m.Add(err)
		}

		return nil, m.NilOrError()
	})
	return err
}

// load loads key either by invoking the getter locally or by sending it to another machine.
func (g *group) load(ctx context.Context, key string, dest transport.Sink) (value transport.ByteView, destPopulated bool, err error) {
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
		var value transport.ByteView
		var err error
		if peer, ok := g.instance.PickPeer(key); ok {

			// metrics duration start
			start := time.Now()

			// get value from peers
			value, err = g.getFromPeer(ctx, peer, key)

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

			if errors.Is(err, &transport.ErrNotFound{}) {
				return nil, err
			}

			if errors.Is(err, &transport.ErrRemoteCall{}) {
				return nil, err
			}

			if g.instance.opts.Logger != nil {
				g.instance.opts.Logger.Error(
					fmt.Sprintf("error retrieving key from peer '%s'", peer.PeerInfo().Address),
					"category", "groupcache",
					"err", err,
					"key", key)
			}

			g.Stats.PeerErrors.Add(1)
			if ctx != nil && ctx.Err() != nil {
				// Return here without attempting to get locally
				// since the context is no longer valid
				return nil, err
			}
		}

		value, err = g.getLocally(ctx, key, dest)
		if err != nil {
			g.Stats.LocalLoadErrs.Add(1)
			return nil, err
		}
		g.Stats.LocalLoads.Add(1)
		destPopulated = true // only one caller of load gets this return value
		g.populateCache(key, value, g.mainCache)
		return value, nil
	})
	if err == nil {
		value = viewi.(transport.ByteView)
	}
	return
}

func (g *group) getLocally(ctx context.Context, key string, dest transport.Sink) (transport.ByteView, error) {
	err := g.getter.Get(ctx, key, dest)
	if err != nil {
		return transport.ByteView{}, err
	}
	return dest.View()
}

func (g *group) getFromPeer(ctx context.Context, peer peer.Client, key string) (transport.ByteView, error) {
	req := &pb.GetRequest{
		Group: &g.name,
		Key:   &key,
	}
	res := &pb.GetResponse{}
	err := peer.Get(ctx, req, res)
	if err != nil {
		return transport.ByteView{}, err
	}

	var expire time.Time
	if res.Expire != nil && *res.Expire != 0 {
		expire = time.Unix(*res.Expire/int64(time.Second), *res.Expire%int64(time.Second))
	}

	value := transport.ByteViewWithExpire(res.Value, expire)

	// Always populate the hot cache
	g.populateCache(key, value, g.hotCache)
	return value, nil
}

func (g *group) setFromPeer(ctx context.Context, peer peer.Client, k string, v []byte, e time.Time) error {
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

func (g *group) removeFromPeer(ctx context.Context, peer peer.Client, key string) error {
	req := &pb.GetRequest{
		Group: &g.name,
		Key:   &key,
	}
	return peer.Remove(ctx, req)
}

func (g *group) lookupCache(key string) (value transport.ByteView, ok bool) {
	if g.cacheBytes <= 0 {
		return
	}
	value, ok = g.mainCache.Get(key)
	if ok {
		return
	}
	value, ok = g.hotCache.Get(key)
	return
}

func (g *group) LocalSet(key string, value []byte, expire time.Time) {
	g.localSet(key, value, expire, g.mainCache)
}

func (g *group) localSet(key string, value []byte, expire time.Time, cache Cache) {
	if g.cacheBytes <= 0 {
		return
	}

	bv := transport.ByteViewWithExpire(value, expire)

	// Ensure no requests are in flight
	g.loadGroup.Lock(func() {
		g.populateCache(key, bv, cache)
	})
}

func (g *group) LocalRemove(key string) {
	// Clear key from our local cache
	if g.cacheBytes <= 0 {
		return
	}

	// Ensure no requests are in flight
	g.loadGroup.Lock(func() {
		g.hotCache.Remove(key)
		g.mainCache.Remove(key)
	})
}

func (g *group) populateCache(key string, value transport.ByteView, cache Cache) {
	if g.cacheBytes <= 0 {
		return
	}
	cache.Add(key, value)
}

// CacheType represents a type of cache.
type CacheType int

const (
	// The MainCache is the cache for items that this peer is the
	// owner for.
	MainCache CacheType = iota + 1

	// The HotCache is the cache for items that seem popular
	// enough to replicate to this node, even though it's not the
	// owner.
	HotCache
)

// CacheStats returns stats about the provided cache within the group.
func (g *group) CacheStats(which CacheType) CacheStats {
	switch which {
	case MainCache:
		return g.mainCache.Stats()
	case HotCache:
		return g.hotCache.Stats()
	default:
		return CacheStats{}
	}
}

// ResetCacheSize changes the maxBytes allowed and resets both the main and hot caches.
// It is mostly intended for testing and is not thread safe.
func (g *group) ResetCacheSize(maxBytes int64) {
	g.cacheBytes = maxBytes
	var (
		hotCache  int64
		mainCache int64
	)

	// Avoid divide by zero
	if maxBytes >= 0 {
		// Hot cache is 1/8th the size of the main cache
		hotCache = maxBytes / 8
		mainCache = hotCache * 7
	}

	g.mainCache = g.instance.opts.CacheFactory(mainCache)
	g.hotCache = g.instance.opts.CacheFactory(hotCache)
}
