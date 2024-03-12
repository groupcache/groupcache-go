package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/groupcache/groupcache-go/v2"
	"github.com/groupcache/groupcache-go/v2/transport"
	"github.com/groupcache/groupcache-go/v2/transport/peer"
)

var _daemons []*Daemon
var _peers []peer.Info

// ListPeers returns a list of all peers in the cluster
func ListPeers() []peer.Info {
	return _peers
}

// ListDaemons returns a list of all daemons in the cluster
func ListDaemons() []*Daemon {
	return _daemons
}

// DaemonAt returns a specific daemon
func DaemonAt(idx int) *Daemon {
	return _daemons[idx]
}

// PeerAt returns a specific peer
func PeerAt(idx int) peer.Info {
	return _peers[idx]
}

// FindOwningDaemon finds the daemon which owns the key provided
func FindOwningDaemon(key string) *Daemon {
	c, isRemote := _daemons[0].GroupCache.PickPeer(key)
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
		d, err := SpawnDaemon(ctx, p.Address, opts)
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
		_ = _daemons[i].GroupCache.SetPeers(ctx, _peers)
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
