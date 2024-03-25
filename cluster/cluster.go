/*
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

/*
Package cluster contains convince functions which make managing the creation of multiple groupcache instances
simple. To start a local cluster of groupcache daemons suitable for testing you can call cluster.Start()
or cluster.StartWith(). See cluster_test.go for more examples.

	err := cluster.Start(context.Background(), 2, groupcache.Options{})
	require.NoError(t, err)

	assert.Equal(t, 2, len(cluster.ListPeers()))
	assert.Equal(t, 2, len(cluster.ListDaemons()))
	err = cluster.Shutdown(context.Background())
	require.NoError(t, err)
*/
package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

var _daemons []*groupcache.Daemon
var _peers []peer.Info

// ListPeers returns a list of all peers in the cluster
func ListPeers() []peer.Info {
	return _peers
}

// ListDaemons returns a list of all daemons in the cluster
func ListDaemons() []*groupcache.Daemon {
	return _daemons
}

// DaemonAt returns a specific daemon
func DaemonAt(idx int) *groupcache.Daemon {
	return _daemons[idx]
}

// PeerAt returns a specific peer
func PeerAt(idx int) peer.Info {
	return _peers[idx]
}

// FindOwningDaemon finds the daemon which owns the key provided
func FindOwningDaemon(key string) *groupcache.Daemon {
	if len(_daemons) == 0 {
		panic("'_daemon' is empty; start a cluster with Start() or StartWith()")
	}

	c, isRemote := _daemons[0].GetInstance().PickPeer(key)
	if !isRemote {
		return _daemons[0]
	}

	for i, d := range _daemons {
		if d.ListenAddress() == c.PeerInfo().Address {
			return _daemons[i]
		}
	}
	panic(fmt.Sprintf("failed to find daemon which owns '%s'", key))
}

// Start a local cluster
func Start(ctx context.Context, numInstances int, opts groupcache.Options) error {
	var peers []peer.Info
	port := 1111
	for i := 0; i < numInstances; i++ {
		peers = append(peers, peer.Info{
			Address: fmt.Sprintf("localhost:%d", port),
		})
		port += 1
	}
	return StartWith(ctx, peers, opts)
}

// StartWith a local cluster with specific addresses
func StartWith(ctx context.Context, peers []peer.Info, opts groupcache.Options) error {
	if len(_daemons) != 0 || len(_peers) != 0 {
		return errors.New("StartWith: cluster already running; shutdown the previous cluster")
	}

	var parent transport.Transport
	if opts.Transport == nil {
		parent = transport.NewHttpTransport(transport.HttpTransportOptions{})
	} else {
		parent = opts.Transport
	}

	for _, p := range peers {
		d, err := groupcache.SpawnDaemon(ctx, p.Address, opts)
		if err != nil {
			return fmt.Errorf("StartWith: while starting daemon for '%s': %w", p.Address, err)
		}

		// Create a new instance of the parent transport
		opts.Transport = parent.New()

		// Add the peers and daemons to the package level variables
		_daemons = append(_daemons, d)
		_peers = append(_peers, peer.Info{
			Address: d.ListenAddress(),
			IsSelf:  p.IsSelf,
		})
	}

	// Tell each daemon about the other peers
	for _, d := range _daemons {
		if err := d.SetPeers(ctx, _peers); err != nil {
			return fmt.Errorf("StartWith: during SetPeers(): %w", err)
		}
	}
	return nil
}

// Restart the cluster
func Restart(ctx context.Context) error {
	for i := 0; i < len(_daemons); i++ {
		if err := _daemons[i].Shutdown(ctx); err != nil {
			return err
		}
		if err := _daemons[i].Start(ctx); err != nil {
			return err
		}
		_ = _daemons[i].GetInstance().SetPeers(ctx, _peers)
	}
	return nil
}

// Shutdown all daemons in the cluster
func Shutdown(ctx context.Context) error {
	for _, d := range _daemons {
		if err := d.Shutdown(ctx); err != nil {
			return err
		}
	}
	_peers = nil
	_daemons = nil
	return nil
}
