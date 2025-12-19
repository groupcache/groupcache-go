/*
Copyright 2012 Google Inc.
Copyright Derrick J Wippler
Copyright 2025 Arsene Tochemey Gandote

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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	instrumentationName = "github.com/groupcache/groupcache-go/instrumentation/otel"
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
	BatchRemoves             AtomicInt // total RemoveKeys requests
	BatchKeysRemoved         AtomicInt // total keys removed via RemoveKeys
}

type MeterProviderOption func(*MeterProvider)

func WithMeterProvider(mp metric.MeterProvider) MeterProviderOption {
	return func(m *MeterProvider) {
		if mp != nil {
			m.underlying = mp
		}
	}
}

type MeterProvider struct {
	underlying metric.MeterProvider
	meter      metric.Meter
}

func NewMeterProvider(opts ...MeterProviderOption) *MeterProvider {
	mp := &MeterProvider{
		underlying: otel.GetMeterProvider(),
	}

	for _, opt := range opts {
		opt(mp)
	}

	mp.meter = mp.underlying.Meter(instrumentationName)
	return mp
}

func (mp *MeterProvider) getMeter() metric.Meter {
	return mp.meter
}

type groupInstruments struct {
	getsCounter                 metric.Int64ObservableCounter
	hitsCounter                 metric.Int64ObservableCounter
	peerLoadsCounter            metric.Int64ObservableCounter
	peerErrorsCounter           metric.Int64ObservableCounter
	loadsCounter                metric.Int64ObservableCounter
	loadsDedupedCounter         metric.Int64ObservableCounter
	localLoadsCounter           metric.Int64ObservableCounter
	localLoadErrsCounter        metric.Int64ObservableCounter
	getFromPeersLatencyMaxGauge metric.Int64ObservableUpDownCounter
	batchRemovesCounter         metric.Int64ObservableCounter
	batchKeysRemovedCounter     metric.Int64ObservableCounter
}

// newGroupInstruments registers all instruments that map to GroupStats counters.
func newGroupInstruments(meter metric.Meter) (*groupInstruments, error) {
	getsCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.gets",
		metric.WithDescription("Total get requests for the group"),
	)
	if err != nil {
		return nil, err
	}

	hitsCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.cache_hits",
		metric.WithDescription("Total successful cache hits"),
	)
	if err != nil {
		return nil, err
	}

	peerLoadsCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.peer.loads",
		metric.WithDescription("Total successful loads from peers"),
	)
	if err != nil {
		return nil, err
	}

	peerErrorsCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.peer.errors",
		metric.WithDescription("Total failed loads from peers"),
	)
	if err != nil {
		return nil, err
	}

	loadsCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.loads",
		metric.WithDescription("Total loads after cache miss"),
	)
	if err != nil {
		return nil, err
	}

	loadsDedupedCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.loads.deduped",
		metric.WithDescription("Total loads de-duplicated by singleflight"),
	)
	if err != nil {
		return nil, err
	}

	localLoadsCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.local.loads",
		metric.WithDescription("Total successful local loads"),
	)
	if err != nil {
		return nil, err
	}

	localLoadErrsCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.local.load_errors",
		metric.WithDescription("Total failed local loads"),
	)
	if err != nil {
		return nil, err
	}

	getFromPeersLatencyMaxGauge, err := meter.Int64ObservableUpDownCounter(
		"groupcache.group.peer.latency_max_ms",
		metric.WithDescription("Maximum latency (ms) observed when loading from peers"),
	)
	if err != nil {
		return nil, err
	}

	batchRemovesCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.batch.removes",
		metric.WithDescription("Total RemoveKeys requests"),
	)
	if err != nil {
		return nil, err
	}

	batchKeysRemovedCounter, err := meter.Int64ObservableCounter(
		"groupcache.group.batch.keys_removed",
		metric.WithDescription("Total keys removed via RemoveKeys"),
	)
	if err != nil {
		return nil, err
	}

	return &groupInstruments{
		getsCounter:                 getsCounter,
		hitsCounter:                 hitsCounter,
		peerLoadsCounter:            peerLoadsCounter,
		peerErrorsCounter:           peerErrorsCounter,
		loadsCounter:                loadsCounter,
		loadsDedupedCounter:         loadsDedupedCounter,
		localLoadsCounter:           localLoadsCounter,
		localLoadErrsCounter:        localLoadErrsCounter,
		getFromPeersLatencyMaxGauge: getFromPeersLatencyMaxGauge,
		batchRemovesCounter:         batchRemovesCounter,
		batchKeysRemovedCounter:     batchKeysRemovedCounter,
	}, nil
}

func (gm *groupInstruments) GetsCounter() metric.Int64ObservableCounter {
	return gm.getsCounter
}

func (gm *groupInstruments) HitsCounter() metric.Int64ObservableCounter {
	return gm.hitsCounter
}

func (gm *groupInstruments) PeerLoadsCounter() metric.Int64ObservableCounter {
	return gm.peerLoadsCounter
}

func (gm *groupInstruments) PeerErrorsCounter() metric.Int64ObservableCounter {
	return gm.peerErrorsCounter
}

func (gm *groupInstruments) LoadsCounter() metric.Int64ObservableCounter {
	return gm.loadsCounter
}

func (gm *groupInstruments) LoadsDedupedCounter() metric.Int64ObservableCounter {
	return gm.loadsDedupedCounter
}

func (gm *groupInstruments) LocalLoadsCounter() metric.Int64ObservableCounter {
	return gm.localLoadsCounter
}

func (gm *groupInstruments) LocalLoadErrsCounter() metric.Int64ObservableCounter {
	return gm.localLoadErrsCounter
}

func (gm *groupInstruments) GetFromPeersLatencyMaxGauge() metric.Int64ObservableUpDownCounter {
	return gm.getFromPeersLatencyMaxGauge
}

func (gm *groupInstruments) BatchRemovesCounter() metric.Int64ObservableCounter {
	return gm.batchRemovesCounter
}

func (gm *groupInstruments) BatchKeysRemovedCounter() metric.Int64ObservableCounter {
	return gm.batchKeysRemovedCounter
}

type cacheInstruments struct {
	rejectedCounter  metric.Int64Counter
	bytesGauge       metric.Int64UpDownCounter
	itemsGauge       metric.Int64UpDownCounter
	getsCounter      metric.Int64Counter
	hitsCounter      metric.Int64Counter
	evictionsCounter metric.Int64Counter
}

func newCacheInstruments(meter metric.Meter) (*cacheInstruments, error) {
	rejectedCounter, err := meter.Int64Counter("groupcache.cache.rejected",
		metric.WithDescription("Total number of items rejected from cache"),
	)
	if err != nil {
		return nil, err
	}

	bytesGauge, err := meter.Int64UpDownCounter(
		"groupcache.cache.bytes",
		metric.WithDescription("Number of bytes in cache"),
	)
	if err != nil {
		return nil, err
	}

	itemsGauge, err := meter.Int64UpDownCounter(
		"groupcache.cache.items",
		metric.WithDescription("Number of items in cache"),
	)
	if err != nil {
		return nil, err
	}

	getsCounter, err := meter.Int64Counter(
		"groupcache.cache.gets",
		metric.WithDescription("Total get requests"),
	)
	if err != nil {
		return nil, err
	}

	hitsCounter, err := meter.Int64Counter(
		"groupcache.cache.hits",
		metric.WithDescription("Total successful cache hits"),
	)
	if err != nil {
		return nil, err
	}

	evictionsCounter, err := meter.Int64Counter(
		"groupcache.cache.evictions",
		metric.WithDescription("Total number of evictions"),
	)
	if err != nil {
		return nil, err
	}

	return &cacheInstruments{
		rejectedCounter:  rejectedCounter,
		bytesGauge:       bytesGauge,
		itemsGauge:       itemsGauge,
		getsCounter:      getsCounter,
		hitsCounter:      hitsCounter,
		evictionsCounter: evictionsCounter,
	}, nil
}

func (cm *cacheInstruments) RejectedCounter() metric.Int64Counter {
	return cm.rejectedCounter
}

func (cm *cacheInstruments) BytesGauge() metric.Int64UpDownCounter {
	return cm.bytesGauge
}

func (cm *cacheInstruments) ItemsGauge() metric.Int64UpDownCounter {
	return cm.itemsGauge
}

func (cm *cacheInstruments) GetsCounter() metric.Int64Counter {
	return cm.getsCounter
}

func (cm *cacheInstruments) HitsCounter() metric.Int64Counter {
	return cm.hitsCounter
}

func (cm *cacheInstruments) EvictionsCounter() metric.Int64Counter {
	return cm.evictionsCounter
}
