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
	RemoveKeys(context context.Context, in *pb.RemoveMultiRequest) error
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

func (e *NoOpClient) RemoveKeys(context context.Context, in *pb.RemoveMultiRequest) error {
	return nil
}

func (e *NoOpClient) PeerInfo() Info {
	return e.Info
}

func (e *NoOpClient) HashKey() string {
	return e.Info.Address
}
