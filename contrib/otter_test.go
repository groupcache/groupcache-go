package contrib_test

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/groupcache/groupcache-go/v3/contrib"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOtterCrud(t *testing.T) {
	c, err := contrib.NewOtterCache(20_000)
	require.NoError(t, err)

	c.Add("key1", transport.ByteViewWithExpire([]byte("value1"), time.Time{}))

	v, ok := c.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", v.String())
	assert.Equal(t, int64(1), c.Stats().Hits)
	assert.Equal(t, int64(1), c.Stats().Gets)
	assert.Equal(t, int64(1), c.Stats().Items)

	// This item should be rejected by otter as it's "cost" is too high
	c.Add("too-large", transport.ByteViewWithExpire(randomValue((20_000/10)+1), time.Time{}))
	assert.Equal(t, int64(1), c.Stats().Rejected)
	assert.Equal(t, int64(1), c.Stats().Items)

	c.Remove("key1")
	assert.Equal(t, int64(1), c.Stats().Hits)
	assert.Equal(t, int64(1), c.Stats().Gets)
	assert.Equal(t, int64(0), c.Stats().Items)
}

func TestOtterEnsureUpdateExpiredValue(t *testing.T) {
	c, err := contrib.NewOtterCache(20_000)
	require.NoError(t, err)
	curTime := time.Now()

	// Override the now function so we control time
	c.Now = func() time.Time {
		return curTime
	}

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

func randomValue(length int) []byte {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	return bytes
}
