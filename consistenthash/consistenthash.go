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

// Package consistenthash provides an implementation of a ring hash.
package consistenthash

import (
	"crypto/md5"
	"fmt"
	"sort"
	"strconv"

	"github.com/segmentio/fasthash/fnv1"
)

type Hash func(data []byte) uint64

type Map struct {
	hash     Hash
	replicas int
	keys     []entry // Sorted
}

type entry struct {
	sum int
	key string // maps sum back to key
}

func New(replicas int, fn Hash) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
	}
	if m.hash == nil {
		m.hash = fnv1.HashBytes64
	}
	return m
}

// IsEmpty returns true if there are no items available.
func (m *Map) IsEmpty() bool {
	return len(m.keys) == 0
}

// Add adds some keys to the hash.
func (m *Map) Add(keys ...string) {
	for _, key := range keys {
		for i := range m.replicas {
			hash := int(m.hash([]byte(fmt.Sprintf("%x", md5.Sum([]byte(strconv.Itoa(i)+key))))))
			m.keys = append(m.keys, entry{sum: hash, key: key})
		}
	}
	sort.Slice(m.keys, func(i, j int) bool { return m.keys[i].sum < m.keys[j].sum })
}

// Get gets the closest item in the hash to the provided key.
func (m *Map) Get(key string) string {
	if m.IsEmpty() {
		return ""
	}

	hash := int(m.hash([]byte(key)))

	// Binary search for appropriate replica.
	idx := sort.Search(len(m.keys), func(i int) bool { return m.keys[i].sum >= hash })

	// Means we have cycled back to the first replica.
	if idx == len(m.keys) {
		idx = 0
	}

	return m.keys[idx].key
}
