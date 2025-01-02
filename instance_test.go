/*
Copyright 2012 Google Inc.
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

package groupcache_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/cluster"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/pb/testpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type TestGroup interface {
	Get(ctx context.Context, key string, dest transport.Sink) error
	CacheStats(which groupcache.CacheType) groupcache.CacheStats
	ResetCacheSize(int64) error
}

var (
	once        sync.Once
	protoGroup  transport.Group
	stringGroup TestGroup

	stringCh = make(chan string)

	dummyCtx context.Context

	// cacheFills is the number of times stringGroup or
	// protoGroup's Getter have been called. Read using the
	// cacheFills function.
	cacheFills groupcache.AtomicInt
)

const (
	stringGroupName = "string-group"
	protoGroupName  = "proto-group"
	expireGroupName = "expire-group"
	fromChan        = "from-chan"
	cacheSize       = 1 << 20
)

func testSetup() {
	instance := groupcache.New(groupcache.Options{})

	g, _ := instance.NewGroup(stringGroupName, cacheSize,
		groupcache.GetterFunc(func(_ context.Context, key string, dest transport.Sink) error {
			if key == fromChan {
				key = <-stringCh
			}
			cacheFills.Add(1)
			return dest.SetString("ECHO:"+key, time.Time{})
		}))
	stringGroup = g.(TestGroup)

	protoGroup, _ = instance.NewGroup(protoGroupName, cacheSize,
		groupcache.GetterFunc(func(_ context.Context, key string, dest transport.Sink) error {
			if key == fromChan {
				key = <-stringCh
			}
			cacheFills.Add(1)
			return dest.SetProto(&testpb.TestMessage{
				Name: proto.String("ECHO:" + key),
				City: proto.String("SOME-CITY"),
			}, time.Time{})
		}))

}

// tests that a Getter's Get method is only called once with two
// outstanding callers.  This is the string variant.
func TestGetDupSuppressString(t *testing.T) {
	once.Do(testSetup)
	// Start two getters. The first should block (waiting reading
	// from stringCh) and the second should latch on to the first
	// one.
	resc := make(chan string, 2)
	for i := 0; i < 2; i++ {
		go func() {
			var s string
			if err := stringGroup.Get(dummyCtx, fromChan, transport.StringSink(&s)); err != nil {
				resc <- "ERROR:" + err.Error()
				return
			}
			resc <- s
		}()
	}

	// Wait a bit so both goroutines get merged together via
	// singleflight.
	// TODO(bradfitz): decide whether there are any non-offensive
	// debug/test hooks that could be added to singleflight to
	// make a sleep here unnecessary.
	time.Sleep(250 * time.Millisecond)

	// Unblock the first getter, which should unblock the second
	// as well.
	stringCh <- "foo"

	for i := 0; i < 2; i++ {
		select {
		case v := <-resc:
			if v != "ECHO:foo" {
				t.Errorf("got %q; want %q", v, "ECHO:foo")
			}
		case <-time.After(5 * time.Second):
			t.Errorf("timeout waiting on getter #%d of 2", i+1)
		}
	}
}

// tests that a Getter's Get method is only called once with two
// outstanding callers.  This is the proto variant.
func TestGetDupSuppressProto(t *testing.T) {
	once.Do(testSetup)
	// Start two getters. The first should block (waiting reading
	// from stringCh) and the second should latch on to the first
	// one.
	resc := make(chan *testpb.TestMessage, 2)
	for i := 0; i < 2; i++ {
		go func() {
			tm := new(testpb.TestMessage)
			if err := protoGroup.Get(dummyCtx, fromChan, transport.ProtoSink(tm)); err != nil {
				tm.Name = proto.String("ERROR:" + err.Error())
			}
			resc <- tm
		}()
	}

	// Wait a bit so both goroutines get merged together via
	// singleflight.
	// TODO(bradfitz): decide whether there are any non-offensive
	// debug/test hooks that could be added to singleflight to
	// make a sleep here unnecessary.
	time.Sleep(250 * time.Millisecond)

	// Unblock the first getter, which should unblock the second
	// as well.
	stringCh <- "Fluffy"
	want := &testpb.TestMessage{
		Name: proto.String("ECHO:Fluffy"),
		City: proto.String("SOME-CITY"),
	}
	for i := 0; i < 2; i++ {
		select {
		case v := <-resc:
			if !proto.Equal(v, want) {
				t.Errorf(" Got: %v\nWant: %v", v.String(), want.String())
			}
		case <-time.After(5 * time.Second):
			t.Errorf("timeout waiting on getter #%d of 2", i+1)
		}
	}
}

func countFills(f func()) int64 {
	fills0 := cacheFills.Get()
	f()
	return cacheFills.Get() - fills0
}
func TestCachingExpire(t *testing.T) {
	var fills int
	instance := groupcache.New(groupcache.Options{})
	g, err := instance.NewGroup(expireGroupName, cacheSize,
		groupcache.GetterFunc(func(_ context.Context, key string, dest transport.Sink) error {
			fills++
			return dest.SetString("ECHO:"+key, time.Now().Add(time.Millisecond*500))
		}))
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		var s string
		if err := g.Get(context.Background(), "TestCachingExpire-key", transport.StringSink(&s)); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			time.Sleep(time.Millisecond * 900)
		}
	}
	if fills != 2 {
		t.Errorf("expected 2 cache fill; got %d", fills)
	}
}

func TestCaching(t *testing.T) {
	once.Do(testSetup)
	fills := countFills(func() {
		for i := 0; i < 10; i++ {
			var s string
			if err := stringGroup.Get(dummyCtx, "TestCaching-key", transport.StringSink(&s)); err != nil {
				t.Fatal(err)
			}
		}
	})
	if fills != 1 {
		t.Errorf("expected 1 cache fill; got %d", fills)
	}
}

func TestCacheEviction(t *testing.T) {
	once.Do(testSetup)
	testKey := "TestCacheEviction-key"
	getTestKey := func() {
		var res string
		for i := 0; i < 10; i++ {
			if err := stringGroup.Get(dummyCtx, testKey, transport.StringSink(&res)); err != nil {
				t.Fatal(err)
			}
		}
	}
	fills := countFills(getTestKey)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill; got %d", fills)
	}

	stats := stringGroup.CacheStats(groupcache.MainCache)
	evict0 := stats.Evictions

	// Trash the cache with other keys.
	var bytesFlooded int64
	// cacheSize/len(testKey) is approximate
	for bytesFlooded < cacheSize+1024 {
		var res string
		key := fmt.Sprintf("dummy-key-%d", bytesFlooded)
		err := stringGroup.Get(dummyCtx, key, transport.StringSink(&res))
		require.NoError(t, err)
		bytesFlooded += int64(len(key) + len(res))
	}
	evicts := stringGroup.CacheStats(groupcache.MainCache).Evictions - evict0
	if evicts <= 0 {
		t.Errorf("evicts = %v; want more than 0", evicts)
	}

	// Test that the key is gone.
	fills = countFills(getTestKey)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill after cache trashing; got %d", fills)
	}
}

const groupName = "group-a"

func TestPeers(t *testing.T) {
	mockTransport := transport.NewMockTransport()
	err := cluster.Start(context.Background(), 3, groupcache.Options{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Transport: mockTransport,
	})
	require.NoError(t, err)
	defer func() { _ = cluster.Shutdown(context.Background()) }()

	var localHits, totalHits int
	newGetter := func(idx int) groupcache.Getter {
		return groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
			totalHits++
			// Only record local (non-remote hits)
			if idx == 0 {
				localHits++
			}
			return dest.SetString("got:"+key, time.Time{})
		})
	}

	var groups []TestGroup
	// Create a group for each instance in the cluster
	for idx, d := range cluster.ListDaemons() {
		g, err := d.GetInstance().NewGroup(groupName, 1<<20, newGetter(idx))
		require.NoError(t, err)
		groups = append(groups, g.(TestGroup))
	}

	resetCacheSize := func(maxBytes int64) {
		for _, g := range groups {
			_ = g.ResetCacheSize(maxBytes)
		}
	}

	run := func(t *testing.T, name string, n int, wantSummary string) {
		t.Helper()
		group := cluster.DaemonAt(0).GetInstance().GetGroup(groupName)

		// Reset counters
		localHits, totalHits = 0, 0
		mockTransport.Reset()

		// Generate the same ip addresses each run
		random := rand.New(rand.NewSource(0))

		for i := 0; i < n; i++ {
			// We use ip addresses for keys as it gives us a much better distribution across peers
			// than fmt.Sprintf("key-%d", i)
			r := random.Int31()
			key := net.IPv4(192, byte(r>>16), byte(r>>8), byte(r)).String()

			var got string
			err := group.Get(context.Background(), key, transport.StringSink(&got))
			if err != nil {
				t.Errorf("%s: error on key %q: %v", name, key, err)
				continue
			}

			want := "got:" + key
			if got != want {
				t.Errorf("%s: for key %q, got %q; want %q", name, key, got, want)
			}
		}

		summary := func() string {
			return fmt.Sprintf("total = %d localhost:1111 = %d %s", totalHits, localHits, mockTransport.Report("Get"))
		}

		if got := summary(); got != wantSummary {
			t.Errorf("%s: got %q; want %q", name, got, wantSummary)
		}
	}

	run(t, "base", 200, "total = 200 localhost:1111 = 96 localhost:1112 = 38 localhost:1113 = 66")
	// Verify cache was hit.  All localHits and peers are gone as the hotCache has the data we need
	run(t, "base_cached", 200, "total = 0 localhost:1111 = 0 ")

	// Force no cache hits
	resetCacheSize(0)

	// With one of the peers being down.
	_ = cluster.DaemonAt(1).Shutdown(context.Background())
	run(t, "one_peer_down", 200, "total = 200 localhost:1111 = 134 localhost:1113 = 66")
}

func TestTruncatingByteSliceTarget(t *testing.T) {
	once.Do(testSetup)

	var buf [100]byte
	s := buf[:]
	if err := stringGroup.Get(dummyCtx, "short", transport.TruncatingByteSliceSink(&s)); err != nil {
		t.Fatal(err)
	}
	if want := "ECHO:short"; string(s) != want {
		t.Errorf("short key got %q; want %q", s, want)
	}

	s = buf[:6]
	if err := stringGroup.Get(dummyCtx, "truncated", transport.TruncatingByteSliceSink(&s)); err != nil {
		t.Fatal(err)
	}
	if want := "ECHO:t"; string(s) != want {
		t.Errorf("truncated key got %q; want %q", s, want)
	}
}

func TestAllocatingByteSliceTarget(t *testing.T) {
	var dst []byte
	sink := transport.AllocatingByteSliceSink(&dst)

	inBytes := []byte("some bytes")
	err := sink.SetBytes(inBytes, time.Time{})
	require.NoError(t, err)

	if want := "some bytes"; string(dst) != want {
		t.Errorf("SetBytes resulted in %q; want %q", dst, want)
	}

	v, err := sink.View()
	if err != nil {
		t.Fatalf("view after SetBytes failed: %v", err)
	}

	// Modify the original "some bytes" slice and the destination
	dst[0] = 'A'
	inBytes[0] = 'B'

	if v.String() != "some bytes" {
		t.Error("inBytes or dst share memory with the view")
	}

	if &inBytes[0] == &dst[0] {
		t.Error("inBytes and dst share memory")
	}
}

func TestNoDeDup(t *testing.T) {
	var totalHits int

	gc := groupcache.New(groupcache.Options{})
	getter := groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
		time.Sleep(time.Second)
		totalHits++
		return dest.SetString("value", time.Time{})
	})

	g, err := gc.NewGroup(groupName, 1<<20, getter)
	require.NoError(t, err)

	resultCh := make(chan string, 100)
	go func() {
		var wg sync.WaitGroup

		for i := 0; i < 100_000; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var s string
				if err := g.Get(dummyCtx, "key", transport.StringSink(&s)); err != nil {
					resultCh <- "ERROR:" + err.Error()
					return
				}
				resultCh <- s
			}()
		}
		wg.Wait()
		close(resultCh)
	}()

	for v := range resultCh {
		if strings.HasPrefix(v, "ERROR:") {
			t.Errorf("Get() returned unexpected error '%s'", v)
		}
	}

	// If the singleflight callback doesn't double-check the cache again
	// upon entry, we would increment nbytes twice but the entry would
	// only be in the cache once.
	const wantBytes = int64(len("key") + len("value"))
	used, _ := g.UsedBytes()
	if used != wantBytes {
		t.Errorf("cache has %d bytes, want %d", used, wantBytes)
	}
}

func TestSetValueOnAllPeers(t *testing.T) {
	ctx := context.Background()
	err := cluster.Start(ctx, 3, groupcache.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	defer func() { _ = cluster.Shutdown(context.Background()) }()

	// Create a group for each instance in the cluster
	var groups []groupcache.Group
	for _, d := range cluster.ListDaemons() {
		g, err := d.GetInstance().NewGroup("group", 1<<20, groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
			return dest.SetString("original-value", time.Time{})
		}))
		require.NoError(t, err)
		groups = append(groups, g)
	}

	// Set the value on the first group
	err = groups[0].Set(ctx, "key", []byte("value"), time.Time{}, false)
	require.NoError(t, err)

	// Verify the value exists on all peers
	for i, g := range groups {
		var result string
		err := g.Get(ctx, "key", transport.StringSink(&result))
		require.NoError(t, err, "Failed to get value from peer %d", i)
		assert.Equal(t, "value", result, "Unexpected value from peer %d", i)
	}

	// Update the value on the second group
	err = groups[1].Set(ctx, "key", []byte("foo"), time.Time{}, false)
	require.NoError(t, err)

	// Verify the value was updated
	for i, g := range groups {
		var result string
		err := g.Get(ctx, "key", transport.StringSink(&result))
		require.NoError(t, err, "Failed to get value from peer %d", i)
		assert.Equal(t, "foo", result, "Unexpected value from peer %d", i)
	}
}
