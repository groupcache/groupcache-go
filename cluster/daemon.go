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
