package cluster_test

import (
	"context"
	"testing"

	"github.com/groupcache/groupcache-go/v2"
	"github.com/groupcache/groupcache-go/v2/cluster"
	"github.com/groupcache/groupcache-go/v2/transport/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartMultipleInstances(t *testing.T) {
	err := cluster.Start(context.Background(), 2, groupcache.Options{})
	require.NoError(t, err)

	assert.Equal(t, 2, len(cluster.ListPeers()))
	assert.Equal(t, 2, len(cluster.ListDaemons()))
	err = cluster.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestRestart(t *testing.T) {
	err := cluster.Start(context.Background(), 2, groupcache.Options{})
	require.NoError(t, err)

	assert.Equal(t, 2, len(cluster.ListPeers()))
	assert.Equal(t, 2, len(cluster.ListDaemons()))
	err = cluster.Restart(context.Background())
	require.NoError(t, err)
	err = cluster.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestStartOneInstance(t *testing.T) {
	err := cluster.Start(context.Background(), 1, groupcache.Options{})
	require.NoError(t, err)

	assert.Equal(t, 1, len(cluster.ListPeers()))
	assert.Equal(t, 1, len(cluster.ListDaemons()))
	err = cluster.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestStartMultipleDaemons(t *testing.T) {
	peers := []peer.Info{
		{Address: "localhost:1111"},
		{Address: "localhost:2222"}}
	err := cluster.StartWith(context.Background(), peers, groupcache.Options{})
	require.NoError(t, err)

	daemons := cluster.ListDaemons()
	assert.Equal(t, 2, len(daemons))
	// If local system uses IPV6 localhost will resolve to ::1 if IPV4 then it will be 127.0.0.1,
	// so we only compare the ports and assume the local part resolved correctly depending on the system.
	assert.Contains(t, daemons[0].ListenAddress(), ":1111")
	assert.Contains(t, daemons[1].ListenAddress(), ":2222")
	assert.Contains(t, cluster.DaemonAt(0).ListenAddress(), ":1111")
	assert.Contains(t, cluster.DaemonAt(1).ListenAddress(), ":2222")
	err = cluster.Shutdown(context.Background())
}

func TestStartWithInvalidPeer(t *testing.T) {
	err := cluster.StartWith(context.Background(), []peer.Info{{Address: "1111"}}, groupcache.Options{})
	assert.Error(t, err)
	assert.Nil(t, cluster.ListPeers())
	assert.Nil(t, cluster.ListDaemons())
}
