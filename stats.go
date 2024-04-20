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
	"strconv"
	"sync/atomic"
)

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
	// Rejected is a counter of the total number of items that were not added to
	// the cache due to some consideration of the underlying cache implementation.
	Rejected int64
	// Bytes is a gauge of how many bytes are in the cache
	Bytes int64
	// Items is a gauge of how many items are in the cache
	Items int64
	// Gets reports the total get requests
	Gets int64
	// Hits reports the total successful cache hits
	Hits int64
	// Evictions reports the total number of evictions
	Evictions int64
}

// GroupStats are per-group statistics.
type GroupStats struct {
	Gets                     AtomicInt // any Get request, including from peers
	CacheHits                AtomicInt // either cache was good
	GetFromPeersLatencyLower AtomicInt // slowest duration to request value from peers
	PeerLoads                AtomicInt // either remote load or remote cache hit (not an error)
	PeerErrors               AtomicInt
	Loads                    AtomicInt // (gets - cacheHits)
	LoadsDeduped             AtomicInt // after singleflight
	LocalLoads               AtomicInt // total good local loads
	LocalLoadErrs            AtomicInt // total bad local loads
}
