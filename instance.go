/*
Copyright 2012 Google Inc.
Copyright 2024 Derrick J. Wippler

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
// Each Get() call first consults its local cache, otherwise delegates
// to the requested key's canonical owner, which then checks its cache
// or finally gets the data.  In the common case, many concurrent
// cache misses across a set of peers for the same key result in just
// one cache fill.
package groupcache

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/groupcache/groupcache-go/v3/internal/singleflight"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// Options is the configuration for this instance of groupcache
type Options struct {
	// HashFn is a function type that is used to calculate a hash used in the hash ring
	// Default is fnv1.HashBytes64
	HashFn peer.HashFn

	// Replicas is the number of replicas that will be used in the hash ring
	// Default is 50
	Replicas int

	// Logger is the logger that will be used by groupcache
	// Default is slog.Default()
	Logger Logger

	// Transport is the transport groupcache will use to communicate with peers in the cluster
	// Default is transport.HttpTransport
	Transport transport.Transport

	// CacheFactory returns a new instance of Cache which will be used by groupcache for both
	// the main and hot cache.
	CacheFactory func(maxBytes int64) (Cache, error)
}

// Instance of groupcache
type Instance struct {
	groups map[string]*group
	mu     sync.RWMutex
	picker *peer.Picker
	opts   Options
}

// New instantiates a new Instance of groupcache with the provided options
func New(opts Options) *Instance {
	if opts.CacheFactory == nil {
		opts.CacheFactory = func(maxBytes int64) (Cache, error) {
			return newMutexCache(maxBytes), nil
		}
	}

	if opts.Transport == nil {
		opts.Transport = transport.NewHttpTransport(transport.HttpTransportOptions{})
	}

	i := &Instance{
		groups: make(map[string]*group),
		opts:   opts,
	}

	// Register our instance with the transport
	i.opts.Transport.Register(i)

	// Create a new peer picker using the provided opts
	i.picker = peer.NewPicker(peer.Options{
		HashFn:   opts.HashFn,
		Replicas: opts.Replicas,
	})

	return i
}

// SetPeers is called by the list of peers changes
func (i *Instance) SetPeers(ctx context.Context, peers []peer.Info) error {
	picker := peer.NewPicker(peer.Options{
		HashFn:   i.opts.HashFn,
		Replicas: i.opts.Replicas,
	})

	// calls to Transport.NewClient() could block or take some time to create a new client
	// As such, we instantiate a new picker and prepare the clients before replacing the active
	// picker.
	var includesSelf bool
	for _, p := range peers {
		if p.IsSelf {
			picker.Add(&peer.NoOpClient{Info: peer.Info{Address: p.Address, IsSelf: true}})
			includesSelf = true
			continue
		}
		client, err := i.opts.Transport.NewClient(ctx, p)
		if err != nil {
			return fmt.Errorf("during Transport.NewClient(): %w", err)
		}
		picker.Add(client)
	}

	if !includesSelf {
		return errors.New("peer.Info{IsSelf: true} missing; peer list must contain the address for this instance")
	}

	i.mu.Lock()
	i.picker = picker
	i.mu.Unlock()
	return nil
}

// PickPeer picks the peer for the provided key in a thread safe manner
func (i *Instance) PickPeer(key string) (peer.Client, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.picker.PickPeer(key)
}

// getAllPeers returns a list of clients for every peer in the cluster in a thread safe manner
func (i *Instance) getAllPeers() []peer.Client {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.picker.GetAll()
}

// NewGroup creates a coordinated group-aware Getter from a Getter.
//
// The returned Getter tries (but does not guarantee) to run only one
// Get call at once for a given key across an entire set of peer
// processes. Concurrent callers both in the local process and in
// other processes receive copies of the answer once the original Get
// completes.
//
// The group name must be unique for each getter.
func (i *Instance) NewGroup(name string, cacheBytes int64, getter Getter) (Group, error) {
	if getter == nil {
		return nil, errors.New("NewGroup(): provided Getter cannot be nil")
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	if _, dup := i.groups[name]; dup {
		return nil, fmt.Errorf("duplicate registration of group '%s'", name)
	}
	g := &group{
		instance:      i,
		name:          name,
		getter:        getter,
		maxCacheBytes: cacheBytes,
		loadGroup:     &singleflight.Group{},
		setGroup:      &singleflight.Group{},
		removeGroup:   &singleflight.Group{},
	}
	if err := g.ResetCacheSize(cacheBytes); err != nil {
		return nil, err
	}
	i.groups[name] = g
	return g, nil
}

// GetGroup returns the named group previously created with NewGroup, or
// nil if there's no such group.
func (i *Instance) GetGroup(name string) transport.Group {
	i.mu.RLock()
	g := i.groups[name]
	i.mu.RUnlock()
	return g
}

// RemoveGroup removes group from group pool
func (i *Instance) RemoveGroup(name string) {
	i.mu.Lock()
	delete(i.groups, name)
	i.mu.Unlock()
}
