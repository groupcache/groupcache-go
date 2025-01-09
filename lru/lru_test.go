/*
Copyright 2013 Google Inc.

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

package lru

import (
	"fmt"
	"testing"
	"time"
)

type simpleStruct struct {
	int
	string
}

type complexStruct struct {
	int
	simpleStruct
}

var getTests = []struct {
	name       string
	keyToAdd   interface{}
	keyToGet   interface{}
	expectedOk bool
}{
	{"string_hit", "myKey", "myKey", true},
	{"string_miss", "myKey", "nonsense", false},
	{"simple_struct_hit", simpleStruct{1, "two"}, simpleStruct{1, "two"}, true},
	{"simple_struct_miss", simpleStruct{1, "two"}, simpleStruct{0, "noway"}, false},
	{"complex_struct_hit", complexStruct{1, simpleStruct{2, "three"}},
		complexStruct{1, simpleStruct{2, "three"}}, true},
}

func TestAdd_evictsOldAndReplaces(t *testing.T) {
	var evictedKey Key
	var evictedValue interface{}
	var evictedExpired bool
	lru := New(0)
	lru.OnEvicted = func(key Key, value interface{}, expired bool) {
		evictedKey = key
		evictedValue = value
		evictedExpired = expired
	}
	lru.Add("myKey", 1234, time.Time{})
	lru.Add("myKey", 1235, time.Time{})

	newVal, ok := lru.Get("myKey")
	if !ok {
		t.Fatalf("%s: cache hit = %v; want %v", t.Name(), ok, !ok)
	}
	if newVal != 1235 {
		t.Fatalf("%s: cache hit = %v; want %v", t.Name(), newVal, 1235)
	}
	if evictedKey != "myKey" {
		t.Fatalf("%s: evictedKey = %v; want %v", t.Name(), evictedKey, "myKey")
	}
	if evictedValue != 1234 {
		t.Fatalf("%s: evictedValue = %v; want %v", t.Name(), evictedValue, 1234)
	}
	if evictedExpired {
		t.Fatalf("%s: evictedExpired = %t; want %t", t.Name(), evictedExpired, false)
	}
}

func TestGet(t *testing.T) {
	for _, tt := range getTests {
		lru := New(0)
		lru.Add(tt.keyToAdd, 1234, time.Time{})
		val, ok := lru.Get(tt.keyToGet)
		if ok != tt.expectedOk {
			t.Fatalf("%s: cache hit = %v; want %v", tt.name, ok, !ok)
		} else if ok && val != 1234 {
			t.Fatalf("%s expected get to return 1234 but got %v", tt.name, val)
		}
	}
}

func TestRemove(t *testing.T) {
	lru := New(0)
	lru.Add("myKey", 1234, time.Time{})
	if val, ok := lru.Get("myKey"); !ok {
		t.Fatal("TestRemove returned no match")
	} else if val != 1234 {
		t.Fatalf("TestRemove failed.  Expected %d, got %v", 1234, val)
	}

	lru.Remove("myKey")
	if _, ok := lru.Get("myKey"); ok {
		t.Fatal("TestRemove returned a removed entry")
	}
}

func TestEvict(t *testing.T) {
	evictedKeys := make([]Key, 0)
	var countNonExpiredAndMemFull int
	onEvictedFun := func(key Key, value interface{}, nonExpiredAndMemFull bool) {
		evictedKeys = append(evictedKeys, key)
		if nonExpiredAndMemFull {
			countNonExpiredAndMemFull++
		}
	}

	lru := New(20)
	lru.OnEvicted = onEvictedFun
	for i := 0; i < 22; i++ {
		lru.Add(fmt.Sprintf("myKey%d", i), 1234, time.Time{})
	}

	if len(evictedKeys) != 2 {
		t.Fatalf("got %d evicted keys; want 2", len(evictedKeys))
	}
	if evictedKeys[0] != Key("myKey0") {
		t.Fatalf("got %v in first evicted key; want %s", evictedKeys[0], "myKey0")
	}
	if evictedKeys[1] != Key("myKey1") {
		t.Fatalf("got %v in second evicted key; want %s", evictedKeys[1], "myKey1")
	}
	if countNonExpiredAndMemFull != 2 {
		t.Fatalf("evicted %d non-expired keys due to mem full, but expected 2",
			countNonExpiredAndMemFull)
	}
}

func TestExpire(t *testing.T) {
	var tests = []struct {
		name       string
		key        interface{}
		expectedOk bool
		expire     time.Duration
		wait       time.Duration
	}{
		{"not-expired", "myKey", true, time.Second * 1, time.Duration(0)},
		{"expired", "expiredKey", false, time.Millisecond * 100, time.Millisecond * 150},
	}

	var countNonExpiredAndMemFull int

	for _, tt := range tests {
		lru := New(0)
		lru.OnEvicted = func(key Key, value interface{}, nonExpiredAndMemFull bool) {
			if nonExpiredAndMemFull {
				countNonExpiredAndMemFull++
			}
		}
		lru.Add(tt.key, 1234, time.Now().Add(tt.expire))
		time.Sleep(tt.wait)
		val, ok := lru.Get(tt.key)
		if ok != tt.expectedOk {
			t.Fatalf("%s: cache hit = %v; want %v", tt.name, ok, !ok)
		} else if ok && val != 1234 {
			t.Fatalf("%s expected get to return 1234 but got %v", tt.name, val)
		}
	}

	if countNonExpiredAndMemFull != 0 {
		t.Fatalf("evicted %d non-expired keys due to mem full, but expected 0",
			countNonExpiredAndMemFull)
	}
}

func TestEvictNonExpired(t *testing.T) {
	const maxKeys = 4
	lru := New(maxKeys)
	var countNonExpiredAndMemFull int
	var evictions int
	lru.OnEvicted = func(key Key, value interface{}, nonExpiredAndMemFull bool) {
		evictions++
		if nonExpiredAndMemFull {
			countNonExpiredAndMemFull++
		}
	}
	lru.Add("key1", 1, time.Now().Add(100*time.Millisecond))
	lru.Add("key1", 1, time.Now().Add(100*time.Millisecond))

	// no expired key, 1 eviction, 0 mem full

	if lru.Len() != 1 {
		t.Fatalf("cache size %d, but expected 1", lru.Len())
	}
	if evictions != 1 {
		t.Fatalf("evictions %d, but expected 1", evictions)
	}
	if countNonExpiredAndMemFull != 0 {
		t.Fatalf("evictions of non-expired keys due to mem full %d, but expected 0",
			countNonExpiredAndMemFull)
	}

	evictions = 0

	lru.Add("key2", 2, time.Now().Add(100*time.Millisecond))
	lru.Add("key3", 3, time.Now().Add(100*time.Millisecond))
	lru.Add("key4", 4, time.Now().Add(100*time.Millisecond))
	lru.Add("key5", 5, time.Now().Add(100*time.Millisecond))
	lru.Add("key6", 6, time.Now().Add(100*time.Millisecond))

	// no expired key, 2 evictions, 2 mem full

	if lru.Len() != maxKeys {
		t.Fatalf("cache size %d, but expected %d", lru.Len(), maxKeys)
	}
	if evictions != 2 {
		t.Fatalf("evictions %d, but expected 2", evictions)
	}
	if countNonExpiredAndMemFull != 2 {
		t.Fatalf("evictions of non-expired keys due to mem full %d, but expected 2",
			countNonExpiredAndMemFull)
	}

	evictions = 0
	countNonExpiredAndMemFull = 0

	lru.RemoveOldest() // evict due mem full

	// no expired key, 1 evictions, 1 mem full

	if lru.Len() != maxKeys-1 {
		t.Fatalf("cache size %d, but expected %d", lru.Len(), maxKeys-1)
	}
	if evictions != 1 {
		t.Fatalf("evictions %d, but expected 1", evictions)
	}
	if countNonExpiredAndMemFull != 1 {
		t.Fatalf("evictions of non-expired keys due to mem full %d, but expected 1",
			countNonExpiredAndMemFull)
	}

	evictions = 0
	countNonExpiredAndMemFull = 0

	time.Sleep(200 * time.Millisecond)
	lru.RemoveOldest() // evict due mem full

	// 1 expired key evicted, 1 mem full

	if lru.Len() != maxKeys-2 {
		t.Fatalf("cache size %d, but expected %d", lru.Len(), maxKeys-2)
	}
	if evictions != 1 {
		t.Fatalf("evictions %d, but expected 1", evictions)
	}
	if countNonExpiredAndMemFull != 0 {
		t.Fatalf("evictions of non-expired keys due to mem full %d, but expected 0",
			countNonExpiredAndMemFull)
	}

}

func TestPurge(t *testing.T) {
	lru := New(0)
	var countNonExpiredAndMemFull int
	var evictions int
	lru.OnEvicted = func(key Key, value interface{}, nonExpiredAndMemFull bool) {
		evictions++
		if nonExpiredAndMemFull {
			countNonExpiredAndMemFull++
		}
	}
	lru.Add("key1", 1, time.Now().Add(50*time.Millisecond))
	lru.Add("key2", 2, time.Now().Add(100*time.Millisecond))
	lru.Add("key3", 3, time.Now().Add(200*time.Millisecond))
	lru.Add("key4", 4, time.Now().Add(500*time.Millisecond))
	if lru.Len() != 4 {
		t.Fatalf("cache size %d, but expected 4", lru.Len())
	}
	time.Sleep(250 * time.Millisecond)
	lru.PurgeExpired = false
	lru.RemoveAllExpired()

	if lru.Len() != 4 {
		t.Fatalf("cache size %d, but expected 4", lru.Len())
	}
	if evictions != 0 {
		t.Fatalf("evictions %d, but expected 0", evictions)
	}
	if countNonExpiredAndMemFull != 0 {
		t.Fatalf("evictions of non-expired keys due to mem full %d, but expected 0", countNonExpiredAndMemFull)
	}

	lru.PurgeExpired = true
	lru.RemoveAllExpired()

	if lru.Len() != 1 {
		t.Fatalf("cache size %d, but expected 1", lru.Len())
	}
	if evictions != 3 {
		t.Fatalf("evictions %d, but expected 3", evictions)
	}
	if countNonExpiredAndMemFull != 0 {
		t.Fatalf("evictions of non-expired keys due to mem full %d, but expected 0", countNonExpiredAndMemFull)
	}
}
