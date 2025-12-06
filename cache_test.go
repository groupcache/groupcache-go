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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/groupcache/groupcache-go/v3/transport"
)

func TestEnsureSizeReportedCorrectly(t *testing.T) {
	c := newMutexCache(0)

	// Add the first value
	bv1 := transport.ByteViewWithExpire([]byte("first"), time.Now().Add(100*time.Second))
	c.Add("key1", bv1)
	v, ok := c.Get("key1")

	// Should be len("key1" + "first") == 9
	assert.True(t, ok)
	assert.True(t, v.Equal(bv1))
	assert.Equal(t, int64(9), c.Bytes())

	// Add a second value
	bv2 := transport.ByteViewWithExpire([]byte("second"), time.Now().Add(200*time.Second))

	c.Add("key2", bv2)
	v, ok = c.Get("key2")

	// Should be len("key2" + "second") == (10 + 9) == 19
	assert.True(t, ok)
	assert.True(t, v.Equal(bv2))
	assert.Equal(t, int64(19), c.Bytes())

	// Replace the first value with a shorter value
	bv3 := transport.ByteViewWithExpire([]byte("3"), time.Now().Add(200*time.Second))

	c.Add("key1", bv3)
	v, ok = c.Get("key1")

	// len("key1" + "3") == 5
	// len("key2" + "second") == 10
	assert.True(t, ok)
	assert.True(t, v.Equal(bv3))
	assert.Equal(t, int64(15), c.Bytes())

	// Replace the second value with a longer value
	bv4 := transport.ByteViewWithExpire([]byte("this-string-is-28-chars-long"), time.Now().Add(200*time.Second))

	c.Add("key2", bv4)
	v, ok = c.Get("key2")

	// len("key1" + "3") == 5
	// len("key2" + "this-string-is-28-chars-long") == 32
	assert.True(t, ok)
	assert.True(t, v.Equal(bv4))
	assert.Equal(t, int64(37), c.Bytes())
}

func TestEnsureUpdateExpiredValue(t *testing.T) {
	c := newMutexCache(20_000)
	curTime := time.Now()

	// Override the now function so we control time
	nowFunc = func() time.Time {
		return curTime
	}
	defer func() {
		nowFunc = time.Now
	}()

	// Expires in 1 second
	c.Add("key1", transport.ByteViewWithExpire([]byte("value1"), curTime.Add(time.Second)))
	_, ok := c.Get("key1")
	assert.True(t, ok)

	// Advance 1.1 seconds into the future
	curTime = curTime.Add(time.Millisecond * 1100)

	// Value should have expired
	_, ok = c.Get("key1")
	assert.False(t, ok)

	// Add a new key that expires in 1 second
	c.Add("key2", transport.ByteViewWithExpire([]byte("value2"), curTime.Add(time.Second)))
	_, ok = c.Get("key2")
	assert.True(t, ok)

	// Advance 0.5 seconds into the future
	curTime = curTime.Add(time.Millisecond * 500)

	// Value should still exist
	_, ok = c.Get("key2")
	assert.True(t, ok)

	// Replace the existing key, this should update the expired time
	c.Add("key2", transport.ByteViewWithExpire([]byte("updated value2"), curTime.Add(time.Second)))
	_, ok = c.Get("key2")
	assert.True(t, ok)

	// Advance 0.6 seconds into the future, which puts us past the initial
	// expired time for key2.
	curTime = curTime.Add(time.Millisecond * 600)

	// Should still exist
	_, ok = c.Get("key2")
	assert.True(t, ok)

	// Advance 1.1 seconds into the future
	curTime = curTime.Add(time.Millisecond * 1100)

	// Should not exist
	_, ok = c.Get("key2")
	assert.False(t, ok)
}

func TestMutexCacheRemoveKeys(t *testing.T) {
	c := newMutexCache(0)

	v1 := transport.ByteViewWithExpire([]byte("first"), time.Time{})
	v2 := transport.ByteViewWithExpire([]byte("second"), time.Time{})
	v3 := transport.ByteViewWithExpire([]byte("third"), time.Time{})

	c.Add("k1", v1)
	c.Add("k2", v2)
	c.Add("k3", v3)

	c.RemoveKeys("k1", "k3", "missing")

	_, ok := c.Get("k1")
	assert.False(t, ok)

	_, ok = c.Get("k3")
	assert.False(t, ok)

	got, ok := c.Get("k2")
	assert.True(t, ok)
	assert.True(t, got.Equal(v2))

	stats := c.Stats()
	assert.Equal(t, int64(len("k2")+len("second")), stats.Bytes)
	assert.Equal(t, int64(1), stats.Items)
	assert.Equal(t, int64(2), stats.Evictions)
}

func TestMutexCacheRemoveKeysSingle(t *testing.T) {
	c := newMutexCache(0)

	v1 := transport.ByteViewWithExpire([]byte("v1"), time.Time{})
	v2 := transport.ByteViewWithExpire([]byte("value"), time.Time{})

	c.Add("k1", v1)
	c.Add("k2", v2)

	c.RemoveKeys("k1")

	stats := c.Stats()
	assert.Equal(t, int64(len("k2")+len("value")), stats.Bytes)
	assert.Equal(t, int64(1), stats.Items)
	assert.Equal(t, int64(1), stats.Evictions)

	_, ok := c.Get("k1")
	assert.False(t, ok)

	got, ok := c.Get("k2")
	assert.True(t, ok)
	assert.True(t, got.Equal(v2))
}
