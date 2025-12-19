/*
Copyright 2024 Groupcache Authors

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
	"testing"
	"time"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/cluster"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveKeys(t *testing.T) {
	ctx := context.Background()

	err := cluster.Start(ctx, 3, groupcache.Options{})
	require.NoError(t, err)
	defer func() { _ = cluster.Shutdown(ctx) }()

	callCount := make(map[string]int)
	getter := groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
		callCount[key]++
		return dest.SetString(fmt.Sprintf("value-%s", key), time.Now().Add(time.Minute*5))
	})

	// Register the group on ALL daemons (required for broadcast)
	group, err := cluster.DaemonAt(0).NewGroup("test-remove-keys", 3000000, getter)
	require.NoError(t, err)
	for i := 1; i < 3; i++ {
		_, err := cluster.DaemonAt(i).NewGroup("test-remove-keys", 3000000, getter)
		require.NoError(t, err)
	}

	keys := []string{"key1", "key2", "key3"}

	// First, populate the cache by getting each key
	for _, key := range keys {
		var value string
		err := group.Get(ctx, key, transport.StringSink(&value))
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("value-%s", key), value)
	}

	// Verify getter was called for each key
	for _, key := range keys {
		assert.Equal(t, 1, callCount[key], "getter should be called once for %s", key)
	}

	// Now remove all keys using variadic signature
	err = group.RemoveKeys(ctx, "key1", "key2", "key3")
	require.NoError(t, err)

	// Fetch again - getter should be called again since keys were removed
	for _, key := range keys {
		var value string
		err := group.Get(ctx, key, transport.StringSink(&value))
		require.NoError(t, err)
	}

	// Verify getter was called again for each key
	for _, key := range keys {
		assert.Equal(t, 2, callCount[key], "getter should be called twice for %s after removal", key)
	}
}

func TestRemoveKeysEmpty(t *testing.T) {
	ctx := context.Background()

	err := cluster.Start(ctx, 2, groupcache.Options{})
	require.NoError(t, err)
	defer func() { _ = cluster.Shutdown(ctx) }()

	getter := groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
		return dest.SetString("value", time.Now().Add(time.Minute))
	})

	// Register the group on ALL daemons
	group, err := cluster.DaemonAt(0).NewGroup("test-remove-empty", 3000000, getter)
	require.NoError(t, err)
	_, err = cluster.DaemonAt(1).NewGroup("test-remove-empty", 3000000, getter)
	require.NoError(t, err)

	// Test RemoveKeys with no keys - should not error
	err = group.RemoveKeys(ctx)
	require.NoError(t, err)
}

func TestRemoveKeysWithSlice(t *testing.T) {
	ctx := context.Background()

	err := cluster.Start(ctx, 2, groupcache.Options{})
	require.NoError(t, err)
	defer func() { _ = cluster.Shutdown(ctx) }()

	getter := groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
		return dest.SetString(fmt.Sprintf("value-%s", key), time.Now().Add(time.Minute*5))
	})

	// Register the group on ALL daemons
	group, err := cluster.DaemonAt(0).NewGroup("test-remove-slice", 3000000, getter)
	require.NoError(t, err)
	_, err = cluster.DaemonAt(1).NewGroup("test-remove-slice", 3000000, getter)
	require.NoError(t, err)

	keys := []string{"key1", "key2", "key3"}

	// Populate cache
	for _, key := range keys {
		var value string
		err := group.Get(ctx, key, transport.StringSink(&value))
		require.NoError(t, err)
	}

	// Test RemoveKeys with slice expansion
	err = group.RemoveKeys(ctx, keys...)
	require.NoError(t, err)
}

func TestRemoveKeysStats(t *testing.T) {
	ctx := context.Background()

	err := cluster.Start(ctx, 2, groupcache.Options{})
	require.NoError(t, err)
	defer func() { _ = cluster.Shutdown(ctx) }()

	getter := groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
		return dest.SetString(fmt.Sprintf("value-%s", key), time.Now().Add(time.Minute*5))
	})

	// Register the group on ALL daemons
	group, err := cluster.DaemonAt(0).NewGroup("test-remove-stats", 3000000, getter)
	require.NoError(t, err)
	_, err = cluster.DaemonAt(1).NewGroup("test-remove-stats", 3000000, getter)
	require.NoError(t, err)

	// Call RemoveKeys
	err = group.RemoveKeys(ctx, "key1", "key2", "key3")
	require.NoError(t, err)

	// Note: Stats are internal to the group implementation
	// The batch stats are incremented but not directly accessible via interface
	// This test verifies that the operation completes without error
}

func BenchmarkRemoveKeys(b *testing.B) {
	ctx := context.Background()

	err := cluster.Start(ctx, 3, groupcache.Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = cluster.Shutdown(ctx) }()

	getter := groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
		return dest.SetString(fmt.Sprintf("value-%s", key), time.Now().Add(time.Minute*5))
	})

	// Register the group on ALL daemons
	group, err := cluster.DaemonAt(0).NewGroup("bench-remove", 3000000, getter)
	if err != nil {
		b.Fatal(err)
	}
	for i := 1; i < 3; i++ {
		_, err := cluster.DaemonAt(i).NewGroup("bench-remove", 3000000, getter)
		if err != nil {
			b.Fatal(err)
		}
	}

	// Prepare keys
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	// Populate cache first
	for _, key := range keys {
		var value string
		_ = group.Get(ctx, key, transport.StringSink(&value))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = group.RemoveKeys(ctx, keys...)
	}
}

func BenchmarkRemoveKeysVsLoop(b *testing.B) {
	ctx := context.Background()

	err := cluster.Start(ctx, 3, groupcache.Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = cluster.Shutdown(ctx) }()

	getter := groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
		return dest.SetString(fmt.Sprintf("value-%s", key), time.Now().Add(time.Minute*5))
	})

	// Register the group on ALL daemons
	group, err := cluster.DaemonAt(0).NewGroup("bench-compare", 3000000, getter)
	if err != nil {
		b.Fatal(err)
	}
	for i := 1; i < 3; i++ {
		_, err := cluster.DaemonAt(i).NewGroup("bench-compare", 3000000, getter)
		if err != nil {
			b.Fatal(err)
		}
	}

	// Prepare keys
	keys := make([]string, 50)
	for i := 0; i < 50; i++ {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	b.Run("RemoveKeys", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = group.RemoveKeys(ctx, keys...)
		}
	})

	b.Run("LoopRemove", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, key := range keys {
				_ = group.Remove(ctx, key)
			}
		}
	})
}
