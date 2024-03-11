package discovery

type Peer struct {
	Address string
	IsSelf  bool
}

// T defines the common interface all discovery mechanisms must follow
type T interface {
	// TODO: decide what this is
}

// K8s implements peer discovery via Kubernetes API calls
type K8s struct {
	// TODO
}

// NewK8s creates a new instance of peer discovery using Kubernetes as the peer discover mechanism.
func NewK8s() T {
	return K8s{}
}

// Static is used to manually set peers using a static config
type Static struct {
}

// SetPeers allows the caller to manually update the list of peers
// while groupcache is running. This call is thread safe.
func (s *Static) SetPeers(peers []Peer) {
	// TODO
}

// NewStatic instantiates a new static peer config with the provided
// list of static peers.
func NewStatic(peers []Peer) *Static {
	return &Static{}
}

// DNS implements peer discovery using SRV records
type DNS struct {
	// TODO
}
