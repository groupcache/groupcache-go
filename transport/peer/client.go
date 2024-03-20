package peer

import (
	"context"
	"github.com/groupcache/groupcache-go/v3/transport/pb"
)

// Client is the interface that must be implemented by a peer.
type Client interface {
	Get(context context.Context, in *pb.GetRequest, out *pb.GetResponse) error
	Remove(context context.Context, in *pb.GetRequest) error
	Set(context context.Context, in *pb.SetRequest) error
	PeerInfo() Info
	HashKey() string
}

// NoOpClient is used as a placeholder in the picker for the local instance. It is returned
// when `PickPeer()` returns isSelf = true
type NoOpClient struct {
	Info Info
}

func (e *NoOpClient) Get(context context.Context, in *pb.GetRequest, out *pb.GetResponse) error {
	return nil
}

func (e *NoOpClient) Remove(context context.Context, in *pb.GetRequest) error {
	return nil
}

func (e *NoOpClient) Set(context context.Context, in *pb.SetRequest) error {
	return nil
}

func (e *NoOpClient) PeerInfo() Info {
	return e.Info
}

func (e *NoOpClient) HashKey() string {
	return e.Info.Address
}
