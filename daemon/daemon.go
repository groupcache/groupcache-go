package daemon

import (
	"context"
	"github.com/groupcache/groupcache-go/v2"
)

type Config struct {
	ListenAddress string
	Config        groupcache.Config
}

// Instance of the daemon
type Instance struct {
}

// Spawn an instance of daemon with the config provided
func Spawn(ctx context.Context, config Config) (*Instance, error) {
	return &Instance{}, nil
}

// NewGroup calls NewGroup on the instance of groupcache encapsulated by this daemon instance.
func (i *Instance) NewGroup(name string, cacheBytes int64, getter groupcache.Getter) *groupcache.Group {
	return &groupcache.Group{}
}

// Shutdown attempts a clean shutdown of the daemon and all related resources.
func (*Instance) Shutdown(context.Context) error {
	return nil
}
