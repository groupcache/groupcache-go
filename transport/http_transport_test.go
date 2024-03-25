/*
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

package transport_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/cluster"
	"github.com/groupcache/groupcache-go/v3/data"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/pb"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const groupName = "group-a"

func TestHttpTransport(t *testing.T) {
	// Start a http server to count the number of non cached hits
	var serverHits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "Hello")
		serverHits++
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// SpawnDaemon a cluster of 4 groupcache instances with HTTP Transport
	require.NoError(t, cluster.Start(ctx, 4, groupcache.Options{
		Transport: transport.NewHttpTransport(transport.HttpTransportOptions{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}),
	}))

	newGetter := func(idx int) groupcache.Getter {
		return groupcache.GetterFunc(func(ctx context.Context, key string, dest data.Sink) error {
			if _, err := http.Get(ts.URL); err != nil {
				t.Logf("HTTP request from getter failed with '%s'", err)
			}
			return dest.SetString(strconv.Itoa(idx)+":"+key, time.Time{})
		})
	}

	// Create a group for each instance in the cluster
	for idx, d := range cluster.ListDaemons() {
		_, err := d.GetInstance().NewGroup(groupName, 1<<20, newGetter(idx))
		require.NoError(t, err)
	}

	// Create new transport with default options
	tr := transport.NewHttpTransport(transport.HttpTransportOptions{})

	// Create a new client to the first peer in the cluster
	c, err := tr.NewClient(ctx, cluster.PeerAt(0))
	require.NoError(t, err)

	// Each new key should result in a new hit to the test server
	for _, key := range testKeys(100) {
		var resp pb.GetResponse
		require.NoError(t, getRequest(ctx, c, groupName, key, &resp))

		// The value should be in the format `instance:key`
		assert.True(t, strings.HasSuffix(string(resp.Value), ":"+key))
	}
	assert.Equal(t, 100, serverHits)

	serverHits = 0

	// Multiple gets on the same key to the owner of the key
	owner := cluster.FindOwningDaemon("new-key")
	for i := 0; i < 2; i++ {
		var resp pb.GetResponse
		require.NoError(t, getRequest(ctx, owner.MustClient(), groupName, "new-key", &resp))
	}
	// Should result in only 1 server get
	assert.Equal(t, 1, serverHits)

	// Remove the key from the owner and we should see another server hit
	var resp pb.GetResponse
	require.NoError(t, removeRequest(ctx, owner.MustClient(), groupName, "new-key"))
	require.NoError(t, getRequest(ctx, owner.MustClient(), groupName, "new-key", &resp))
	assert.Equal(t, 2, serverHits)

	// Remove the key, and set it with a different value
	require.NoError(t, removeRequest(ctx, owner.MustClient(), groupName, "new-key"))
	require.NoError(t, setRequest(ctx, owner.MustClient(), groupName, "new-key", "new-value"))

	require.NoError(t, getRequest(ctx, owner.MustClient(), groupName, "new-key", &resp))
	assert.Equal(t, []byte("new-value"), resp.Value)
	// Should not see any new server hits
	assert.Equal(t, 2, serverHits)
}

func getRequest(ctx context.Context, c peer.Client, group, key string, resp *pb.GetResponse) error {
	req := &pb.GetRequest{
		Group: &group,
		Key:   &key,
	}
	return c.Get(ctx, req, resp)
}

func setRequest(ctx context.Context, c peer.Client, group, key, value string) error {
	req := &pb.SetRequest{
		Value: []byte(value),
		Group: &group,
		Key:   &key,
	}
	return c.Set(ctx, req)
}

func removeRequest(ctx context.Context, c peer.Client, group, key string) error {
	req := &pb.GetRequest{
		Group: &group,
		Key:   &key,
	}
	return c.Remove(ctx, req)
}

func testKeys(n int) (keys []string) {
	keys = make([]string, n)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	return
}
