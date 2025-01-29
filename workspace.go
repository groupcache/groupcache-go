package groupcache

import (
	"bytes"
	"sync"
)

// Workspace holds the "global" state for groupcache.
type Workspace struct {
	httpPoolMade bool
	portPicker   func(groupName string) PeerPicker

	mu     sync.RWMutex
	groups map[string]*Group

	bufferPool sync.Pool
}

// DefaultWorkspace is the default workspace, useful for tests.
// If your application does not need to recreate groupcache resources,
// you can use this default workspace as well.
var DefaultWorkspace = NewWorkspace()

// NewWorkspace creates new workspace.
func NewWorkspace() *Workspace {
	return &Workspace{
		groups: make(map[string]*Group),
		bufferPool: sync.Pool{
			New: func() interface{} { return new(bytes.Buffer) },
		},
	}
}
