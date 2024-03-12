/*
Copyright 2013 Google Inc.
Copyright 2024 Derrick J. Wippler

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

package peer_test

import (
	"fmt"
	"math/rand"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/groupcache/groupcache-go/v2/transport/peer"
	"github.com/segmentio/fasthash/fnv1"
	"github.com/stretchr/testify/assert"
)

func TestHashing(t *testing.T) {
	picker := peer.NewPicker(peer.Options{Replicas: 512})

	picker.Add(&peer.NoOpClient{Info: peer.Info{Address: "6"}})
	picker.Add(&peer.NoOpClient{Info: peer.Info{Address: "4"}})
	picker.Add(&peer.NoOpClient{Info: peer.Info{Address: "2"}})

	testCases := map[string]string{
		"12,000":    "4",
		"11":        "6",
		"500,000":   "4",
		"1,000,000": "2",
	}

	for k, v := range testCases {
		if got := picker.Get(k); got.HashKey() != v {
			t.Errorf("Asking for %s, should have yielded %s; got %s instead", k, v, got)
		}
	}

	picker.Add(&peer.NoOpClient{Info: peer.Info{Address: "8"}})

	testCases["11"] = "8"
	testCases["1,000,000"] = "8"

	for k, v := range testCases {
		if got := picker.Get(k); got.HashKey() != v {
			t.Errorf("Asking for %s, should have yielded %s; got %s instead", k, v, got)
		}
	}
}

func TestConsistency(t *testing.T) {
	picker1 := peer.NewPicker(peer.Options{Replicas: 1})
	picker2 := peer.NewPicker(peer.Options{Replicas: 1})

	picker1.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bill"}})
	picker1.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bob"}})
	picker1.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bonny"}})

	picker2.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bob"}})
	picker2.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bonny"}})
	picker2.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bill"}})

	if picker1.Get("Ben").HashKey() != picker2.Get("Ben").HashKey() {
		t.Errorf("Fetching 'Ben' from both hashes should be the same")
	}

	picker2.Add(&peer.NoOpClient{Info: peer.Info{Address: "Becky"}})
	picker2.Add(&peer.NoOpClient{Info: peer.Info{Address: "Ben"}})
	picker2.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bobby"}})

	picker1.Add(&peer.NoOpClient{Info: peer.Info{Address: "Becky"}})
	picker1.Add(&peer.NoOpClient{Info: peer.Info{Address: "Ben"}})
	picker1.Add(&peer.NoOpClient{Info: peer.Info{Address: "Bobby"}})

	if picker1.Get("Ben").HashKey() != picker2.Get("Ben").HashKey() ||
		picker1.Get("Bob").HashKey() != picker2.Get("Bob").HashKey() ||
		picker1.Get("Bonny").HashKey() != picker2.Get("Bonny").HashKey() {
		t.Errorf("Direct matches should always return the same entry")
	}
}

func TestDistribution(t *testing.T) {
	hosts := []string{"a.svc.local", "b.svc.local", "c.svc.local"}
	rand.Seed(time.Now().Unix())
	const cases = 10000

	strings := make([]string, cases)

	for i := 0; i < cases; i++ {
		r := rand.Int31()
		ip := net.IPv4(192, byte(r>>16), byte(r>>8), byte(r))
		strings[i] = ip.String()
	}

	hashFuncs := map[string]peer.HashFn{
		"fasthash/fnv1": fnv1.HashBytes64,
	}

	for name, hashFunc := range hashFuncs {
		t.Run(name, func(t *testing.T) {
			picker := peer.NewPicker(peer.Options{Replicas: 512, HashFn: hashFunc})
			hostMap := map[string]int{}

			for _, host := range hosts {
				picker.Add(&peer.NoOpClient{Info: peer.Info{Address: host}})
				hostMap[host] = 0
			}

			for i := range strings {
				host := picker.Get(strings[i]).HashKey()
				hostMap[host]++
			}

			for host, a := range hostMap {
				t.Logf("host: %s, percent: %f", host, float64(a)/cases)
			}
		})
	}
}

func TestPickPeer(t *testing.T) {
	picker := peer.NewPicker(peer.Options{Replicas: 512})

	for _, info := range []peer.Info{
		{
			Address: "a.svc.local",
			IsSelf:  true,
		}, {
			Address: "b.svc.local",
			IsSelf:  false,
		}, {
			Address: "c.svc.local",
			IsSelf:  false,
		}} {
		picker.Add(&peer.NoOpClient{Info: info})
	}

	p, isRemote := picker.PickPeer("Bob")
	assert.Equal(t, "a.svc.local", p.PeerInfo().Address)
	assert.True(t, p.PeerInfo().IsSelf)
	assert.False(t, isRemote)

	p, isRemote = picker.PickPeer("Johnny")
	assert.Equal(t, "b.svc.local", p.PeerInfo().Address)
	assert.False(t, p.PeerInfo().IsSelf)
	assert.True(t, isRemote)

	p, isRemote = picker.PickPeer("Rick")
	assert.Equal(t, "c.svc.local", p.PeerInfo().Address)
	assert.False(t, p.PeerInfo().IsSelf)
	assert.True(t, isRemote)
}

func TestGetAll(t *testing.T) {
	picker := peer.NewPicker(peer.Options{Replicas: 512})

	for _, info := range []peer.Info{
		{
			Address: "a.svc.local",
			IsSelf:  true,
		}, {
			Address: "b.svc.local",
			IsSelf:  false,
		}, {
			Address: "c.svc.local",
			IsSelf:  false,
		}} {
		picker.Add(&peer.NoOpClient{Info: info})
	}

	all := picker.GetAll()
	assert.Len(t, all, 3)
	assert.True(t, slices.ContainsFunc(all, func(c peer.Client) bool { return c.PeerInfo().Address == "a.svc.local" }))
	assert.True(t, slices.ContainsFunc(all, func(c peer.Client) bool { return c.PeerInfo().Address == "b.svc.local" }))
	assert.True(t, slices.ContainsFunc(all, func(c peer.Client) bool { return c.PeerInfo().Address == "c.svc.local" }))
}

func BenchmarkGet8(b *testing.B)   { benchmarkGet(b, 8) }
func BenchmarkGet32(b *testing.B)  { benchmarkGet(b, 32) }
func BenchmarkGet128(b *testing.B) { benchmarkGet(b, 128) }
func BenchmarkGet512(b *testing.B) { benchmarkGet(b, 512) }

func benchmarkGet(b *testing.B, shards int) {

	picker := peer.NewPicker(peer.Options{Replicas: 50})

	var buckets []string
	for i := 0; i < shards; i++ {
		buckets = append(buckets, fmt.Sprintf("shard-%d", i))
		picker.Add(&peer.NoOpClient{Info: peer.Info{Address: fmt.Sprintf("shard-%d", i)}})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		picker.Get(buckets[i&(shards-1)])
	}
}
