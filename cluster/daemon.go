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

package cluster

import (
	"context"
	"log/slog"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

// Daemon is an instance of groupcache
type Daemon struct {
	GroupCache *groupcache.Instance
	opts       groupcache.Options
	address    string
}

// SpawnDaemon starts a new instance of daemon with the config provided
func SpawnDaemon(ctx context.Context, address string, opts groupcache.Options) (*Daemon, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	if opts.Transport == nil {
		opts.Transport = transport.NewHttpTransport(transport.HttpTransportOptions{})
	}

	i := &Daemon{
		address: address,
		opts:    opts,
	}

	return i, i.Start(ctx)
}

func (i *Daemon) Start(ctx context.Context) error {
	i.GroupCache = groupcache.New(i.opts)
	return i.opts.Transport.SpawnServer(ctx, i.address)
}

// SetPeers tells groupcache about other peers in the cluster, and marks this instance
// as self.
func (i *Daemon) SetPeers(ctx context.Context, src []peer.Info) error {
	dest := make([]peer.Info, len(src))
	for idx := 0; idx < len(src); idx++ {
		dest[idx] = src[idx]
		if dest[idx].Address == i.opts.Transport.ListenAddress() {
			dest[idx].IsSelf = true
		}
	}
	return i.GroupCache.SetPeers(ctx, dest)
}

// Client returns a client for this instance
func (i *Daemon) Client() peer.Client {
	c, err := i.opts.Transport.NewClient(context.Background(), peer.Info{Address: i.opts.Transport.ListenAddress()})
	if err != nil {
		panic(err)
	}
	return c
}

// ListenAddress returns the address this instance is listening on
func (i *Daemon) ListenAddress() string {
	return i.opts.Transport.ListenAddress()
}

// Shutdown attempts a clean shutdown of the daemon and all related resources.
func (i *Daemon) Shutdown(ctx context.Context) error {
	return i.opts.Transport.ShutdownServer(ctx)
}
