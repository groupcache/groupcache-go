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

package transport

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/groupcache/groupcache-go/v3/transport/pb"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

// MockTransport is intended to be used as a singleton. Pass a new instance of this singleton into groupcache.New()
// by calling MockTransport.New(). You can then inspect the parent MockTransport for call statistics of the children
// in tests. This Transport is NOT THREAD SAFE
// Example usage:
//
// t := NewMockTransport()
// i := groupcache.New(groupcache.Options{Transport: t.New()})
type MockTransport struct {
	instances  map[string]GroupCacheInstance
	transports map[string]*MockTransport
	calls      map[string]*peerStats
	register   GroupCacheInstance
	parent     *MockTransport
	address    string
}

func NewMockTransport() *MockTransport {
	m := &MockTransport{
		instances:  make(map[string]GroupCacheInstance),
		transports: make(map[string]*MockTransport),
		calls:      make(map[string]*peerStats),
	}
	// We do this to avoid accidental nil deref errors if MockTransport.New() is never called.
	m.parent = m
	return m
}

func (t *MockTransport) Register(i GroupCacheInstance) {
	t.register = i
}
func (t *MockTransport) New() Transport {
	m := NewMockTransport()
	// Register us as a parent of the new transport
	m.parent = t
	return m
}

func (t *MockTransport) ListenAddress() string {
	return t.address
}

func (t *MockTransport) SpawnServer(_ context.Context, address string) error {
	t.parent.instances[address] = t.register
	t.parent.transports[address] = t
	t.address = address
	return nil
}

func (t *MockTransport) ShutdownServer(_ context.Context) error {
	delete(t.parent.instances, t.address)
	delete(t.parent.transports, t.address)
	return nil
}

func (t *MockTransport) Reset() {
	t.calls = make(map[string]*peerStats)
}

func (t *MockTransport) Report(method string) string {
	stats, ok := t.calls[method]
	if !ok {
		return ""
	}
	return stats.Report()
}

func (t *MockTransport) NewClient(ctx context.Context, peer peer.Info) (peer.Client, error) {
	return &MockClient{
		peer:      peer,
		transport: t,
	}, nil
}

type MockClient struct {
	transport *MockTransport
	peer      peer.Info
}

func (c *MockClient) addCall(method string, count int) {
	m, ok := c.transport.parent.calls[method]
	if !ok {
		c.transport.parent.calls[method] = &peerStats{
			stats: make(map[string]int),
		}
		m = c.transport.parent.calls[method]
	}
	m.Add(c.peer.Address, count)
}

func (c *MockClient) Get(ctx context.Context, in *pb.GetRequest, out *pb.GetResponse) error {
	g, ok := c.transport.parent.instances[c.peer.Address]
	if !ok {
		return fmt.Errorf("dial tcp %s connect: connection refused'", c.peer.Address)
	}

	c.addCall("Get", 1)

	var b []byte
	value := AllocatingByteSliceSink(&b)
	if err := g.GetGroup(in.GetGroup()).Get(ctx, in.GetKey(), value); err != nil {
		return err
	}
	out.Value = b
	return nil
}

func (c *MockClient) Remove(ctx context.Context, in *pb.GetRequest) error {
	c.addCall("Remove", 1)
	// TODO: Implement when needed
	return nil
}

func (c *MockClient) Set(ctx context.Context, in *pb.SetRequest) error {
	c.addCall("Set", 1)
	// TODO: Implement when needed
	return nil
}

func (c *MockClient) PeerInfo() peer.Info {
	c.addCall("PeerInfo", 1)
	return c.peer
}

func (c *MockClient) HashKey() string {
	c.addCall("HashKey", 1)
	return c.peer.Address
}

type peerStats struct {
	stats map[string]int
}

// Add adds a count to the peerStats Map
func (s *peerStats) Add(key string, count int) {
	s.stats[key] += count
}

// Report returns a string representation of the stats in the format <peer-name>:<count>
// Example: "peer1:50 peer2:48 peer3:45"
func (s *peerStats) Report() string {
	var b strings.Builder

	var sorted []string
	for k := range s.stats {
		sorted = append(sorted, k)
	}

	// Map keys have no guaranteed order, so we sort the keys here.
	slices.Sort(sorted)
	for i := 0; i < len(sorted); i++ {
		b.WriteString(fmt.Sprintf("%s = %d ", sorted[i], s.stats[sorted[i]]))
	}
	return strings.TrimSpace(b.String())
}
