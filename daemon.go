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

package groupcache

import (
	"context"
	"log/slog"

	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

// Daemon is an instance of groupcache bound to a port listening for requests
type Daemon struct {
	instance *Instance
	opts     Options
	address  string
}

// ListenAndServe creates a new instance of groupcache listening on the address provided
func ListenAndServe(ctx context.Context, address string, opts Options) (*Daemon, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	if opts.Transport == nil {
		opts.Transport = transport.NewHttpTransport(transport.HttpTransportOptions{})
	}

	daemon := &Daemon{
		address: address,
		opts:    opts,
	}

	return daemon, daemon.Start(ctx)
}

func (d *Daemon) Start(ctx context.Context) error {
	d.instance = New(d.opts)
	return d.opts.Transport.ListenAndServe(ctx, d.address)
}

// NewGroup is a convenience method which calls NewGroup on the instance associated with this daemon.
func (d *Daemon) NewGroup(name string, cacheBytes int64, getter Getter) (transport.Group, error) {
	return d.instance.NewGroup(name, cacheBytes, getter)
}

// GetGroup is a convenience method which calls GetGroup on the instance associated with this daemon
func (d *Daemon) GetGroup(name string) transport.Group {
	return d.instance.GetGroup(name)
}

// RemoveGroup is a convenience method which calls RemoveGroup on the instance associated with this daemon
func (d *Daemon) RemoveGroup(name string) {
	d.instance.RemoveGroup(name)
}

// GetInstance returns the current groupcache instance associated with this daemon
func (d *Daemon) GetInstance() *Instance {
	return d.instance
}

// SetPeers is a convenience method which calls SetPeers on the instance associated with this daemon. In
// addition, it finds and marks this instance as self by asking the transport for it's listening address
// before calling SetPeers() on the instance. If this is not desirable, call Daemon.GetInstance().SetPeers()
// instead.
func (d *Daemon) SetPeers(ctx context.Context, src []peer.Info) error {
	dest := make([]peer.Info, len(src))
	for idx := 0; idx < len(src); idx++ {
		dest[idx] = src[idx]
		if dest[idx].Address == d.ListenAddress() {
			dest[idx].IsSelf = true
		}
	}
	return d.instance.SetPeers(ctx, dest)
}

// MustClient is a convenience method which creates a new client for this instance. This method will
// panic if transport.NewClient() returns an error.
func (d *Daemon) MustClient() peer.Client {
	c, err := d.opts.Transport.NewClient(context.Background(), peer.Info{Address: d.ListenAddress()})
	if err != nil {
		panic(err)
	}
	return c
}

// ListenAddress returns the address this instance is listening on
func (d *Daemon) ListenAddress() string {
	return d.opts.Transport.ListenAddress()
}

// Shutdown attempts a clean shutdown of the daemon and all related resources.
func (d *Daemon) Shutdown(ctx context.Context) error {
	return d.opts.Transport.Shutdown(ctx)
}
