package transport_test

import (
	"context"
	"fmt"
	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/cluster"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/pb"
	"github.com/kapetan-io/tackle/autotls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// AutoGenerate TLS certs
	conf := autotls.Config{AutoTLS: true}
	require.NoError(t, autotls.Setup(&conf))

	// Start a 2 node cluster with TLS
	err := cluster.Start(context.Background(), 2,
		groupcache.Options{
			Transport: transport.NewHttpTransport(transport.HttpTransportOptions{
				TLSConfig: conf.ServerTLS,
				Client: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: conf.ClientTLS,
					},
				},
			}),
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	require.NoError(t, err)

	assert.Equal(t, 2, len(cluster.ListPeers()))
	assert.Equal(t, 2, len(cluster.ListDaemons()))

	// Start a http server to count the number of non cached hits
	var serverHits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "Hello")
		serverHits++
	}))
	defer ts.Close()

	// Create a group on each instance in the cluster
	for idx, d := range cluster.ListDaemons() {
		_, err := d.GetInstance().NewGroup(groupName, 1<<20,
			groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
				if _, err := http.Get(ts.URL); err != nil {
					t.Logf("HTTP request from getter failed with '%s'", err)
				}
				return dest.SetString(strconv.Itoa(idx)+":"+key, time.Time{})
			}))
		require.NoError(t, err)
	}

	// Create new transport with the client TLS config
	tr := transport.NewHttpTransport(transport.HttpTransportOptions{
		TLSConfig: conf.ServerTLS,
		Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: conf.ClientTLS,
			},
		},
	})

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

	require.NoError(t, cluster.Shutdown(context.Background()))
}
