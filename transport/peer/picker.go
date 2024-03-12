/*
Copyright 2013 Google Inc.
Copyright 2024 Derrick J Wippler

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

package peer

import (
	"crypto/md5"
	"fmt"
	"sort"
	"strconv"

	"github.com/segmentio/fasthash/fnv1"
)

const defaultReplicas = 50

// Info represents information about a peer. This struct is intended to be used by peer discovery mechanisms
// when calling `Instance.SetPeers()`
type Info struct {
	Address string
	IsSelf  bool
}

// HashFn represents a function that takes a byte slice as input and returns a uint64 hash value.
type HashFn func(data []byte) uint64

// Options represents the settings for the peer picker
type Options struct {
	// - HashFn: a function type that takes a byte slice and returns a uint64
	HashFn HashFn
	// - Replicas: an integer representing the number of replicas
	Replicas int
}

// Picker represents a data structure for picking a peer based on a given key.
// It uses consistent hashing to distribute keys among the available peers.
type Picker struct {
	opts Options
	// keys is a stored list of all keys in the ring
	keys []int
	// hashMap contains the key ring
	hashMap map[int]Client
	// clientMap contains a list of added clients by key
	clientMap map[string]Client
}

// NewPicker creates a new Picker instance with the given options.
// It initializes the replicas, hash function, and hash map.
// If the hash function is not provided in the options, it defaults to fnv1.HashBytes64.
// Returns the newly created Picker instance.
//
// Example usage:
//
//	picker := NewPicker(Options{
//	    HashFn:   hashFunction,
//	    Replicas: 100,
//	})
//	...
func NewPicker(opts Options) *Picker {
	m := &Picker{
		opts:      opts,
		hashMap:   make(map[int]Client),
		clientMap: make(map[string]Client),
	}
	if m.opts.HashFn == nil {
		m.opts.HashFn = fnv1.HashBytes64
	}
	if m.opts.Replicas == 0 {
		m.opts.Replicas = defaultReplicas
	}
	return m
}

// PickPeer picks the appropriate peer for the given key. Returns true if the chosen client is to a remote peer.
func (p *Picker) PickPeer(key string) (client Client, isRemote bool) {
	if p.IsEmpty() {
		return nil, false
	}

	c := p.Get(key)
	return c, !c.PeerInfo().IsSelf
}

// GetAll returns all clients added to the Picker
func (p *Picker) GetAll() []Client {
	var results []Client
	for _, v := range p.clientMap {
		results = append(results, v)
	}
	return results
}

// IsEmpty returns true if there are no items available.
func (p *Picker) IsEmpty() bool {
	return len(p.keys) == 0
}

// Add a client to the peer picker
func (p *Picker) Add(client Client) {
	p.clientMap[client.HashKey()] = client
	for i := 0; i < p.opts.Replicas; i++ {
		hash := int(p.opts.HashFn([]byte(fmt.Sprintf("%x", md5.Sum([]byte(strconv.Itoa(i)+client.HashKey()))))))
		p.keys = append(p.keys, hash)
		p.hashMap[hash] = client
	}
	sort.Ints(p.keys)
}

// Get the closest client in the hash to the provided key.
func (p *Picker) Get(key string) Client {
	if p.IsEmpty() {
		return nil
	}

	hash := int(p.opts.HashFn([]byte(key)))

	// Binary search for appropriate replica.
	idx := sort.Search(len(p.keys), func(i int) bool { return p.keys[i] >= hash })

	// Means we have cycled back to the first replica.
	if idx == len(p.keys) {
		idx = 0
	}

	return p.hashMap[p.keys[idx]]
}
