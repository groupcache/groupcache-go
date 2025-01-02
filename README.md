
<h2 align="center">
<img src="docs/groupcache-logo.png" alt="GroupCache Logo" width="800" /><br />
Distributed Cache Library
</h2>

---

[![CI](https://github.com/groupcache/groupcache-go/workflows/CI/badge.svg)](https://github.com/groupcache/groupcache-go/actions?query=workflow:"CI")
[![GitHub tag](https://img.shields.io/github/tag/groupcache/groupcache-go?include_prereleases=&sort=semver&color=blue)](https://github.com/groupcache/groupcache-go/releases/)
[![License](https://img.shields.io/badge/License-Apache-blue)](#license)

Groupcache is a Go-based caching and cache-filling library designed to replace traditional caching solutions
like MEMCACHED and REDIS in many scenarios.

## Why Use Groupcache?
- Cost Reduction: Eliminates the need for external system dependencies and additional server infrastructure
- Increased efficiency: Minimizes network calls to external systems and data sources
- Load Protection: Prevents the thundering herd problem through the use of `singleflight` synchronization

## How It Works
Groupcache functions as an in-memory read-through cache with the following workflow:
- When your application requests data, Groupcache uses key sharding to determine key ownership
- For locally-owned keys:
    - If the key exists in the local cache, data is returned immediately
    - If the key is missing, Groupcache retrieves it from the data source, caches it, and returns it
    - Future requests for the same key are served directly from the hot cache
- For keys owned by other peers:
    - Groupcache forwards the request to the appropriate peer instance
    - The owning peer handles the cache lookup and retrieval process

This architecture provides the following benefits:
- Network efficiency by avoiding requests for locally-owned keys
- Load protection by channeling identical key requests to a single Groupcache instance within the cluster
- Avoid unnecessary network calls to external cache systems by using local memory for the cache

Thanks to `singleflight` synchronization, even if your application makes millions of requests for a 
specific key (e.g., "FOO"), Groupcache will only query the underlying data source once.

<div align="center">
<img src="docs/simplified-diagram.png" alt="Simplified Diagram" width="500" /><br />
</div>

## Usage

```go
import (
    "context"
    "fmt"
    "log"
    "time"
    "log/slog"

    "github.com/groupcache/groupcache-go/v3"
    "github.com/groupcache/groupcache-go/v3/transport"
    "github.com/groupcache/groupcache-go/v3/transport/peer"
)

func ExampleUsage() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
    defer cancel()

    // ListenAndServe is a convenience function which Starts an instance of groupcache 
    // with the provided transport and listens for groupcache HTTP requests on
    // the address provided.
    d, err := groupcache.ListenAndServe(ctx, "192.168.1.1:8080", groupcache.Options{})
    if err != nil {
        log.Fatal("while starting server on 192.168.1.1:8080")
    }

    // Manually set peers, or use some discovery system to identify peers.
    // It is safe to call SetPeers() whenever the peer topology changes
    d.SetPeers(ctx, []peer.Info{
        {
            Address: "192.168.1.1:8080",
            IsSelf:  true,
        },
        {
            Address: "192.168.1.1:8081",
            IsSelf:  false,
        },
        {
            Address: "192.168.1.1:8082",
            IsSelf:  false,
        },
    })

    // Create a new group cache with a max cache size of 3MB
    group, err := d.NewGroup("users", 3000000, groupcache.GetterFunc(
        func(ctx context.Context, id string, dest transport.Sink) error {
            // In a real scenario we might fetch the value from a database.
            /*if user, err := fetchUserFromMongo(ctx, id); err != nil {
                return err
            }*/

            user := User{
                Id:      "12345",
                Name:    "John Doe",
                Age:     40,
            }

            // Set the user in the groupcache to expire after 5 minutes
            if err := dest.SetProto(&user, time.Now().Add(time.Minute*5)); err != nil {
                return err
            }
            return nil
        },
    ))
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel = context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    var user User
    if err := group.Get(ctx, "12345", transport.ProtoSink(&user)); err != nil {
        log.Fatal(err)
    }

    fmt.Printf("-- User --\n")
    fmt.Printf("Id: %s\n", user.Id)
    fmt.Printf("Name: %s\n", user.Name)
    fmt.Printf("Age: %d\n", user.Age)

    // Remove the key from the groupcache
    if err := group.Remove(ctx, "12345"); err != nil {
        log.Fatal(err)
    }

    // Shutdown the instance and HTTP listeners
    d.Shutdown(ctx)
}
```
# Concepts
### Groups
GroupCache provides functionality to create multiple distinct "groups" through its `NewGroup()` function. 
Each group serves as a separate namespace or storage pool that is distributed across peers using consistent hashing.
You should create a new group whenever you need to prevent key conflicts between similar or identical key spaces.

### GetterFunc
When creating a new group with `NewGroup()`, you must provide a `groupcache.GetterFunc`. This function serves as the
read-through mechanism for retrieving cache values that aren't present in the local cache. The function works in
conjunction with `singleflight` to ensure that only one request for a specific key is processed at a time. Any 
additional requests for the same key will wait until the initial `groupcache.GetterFunc` call completes. Given 
this behavior, it's crucial to utilize the provided `context.Context` to properly handle timeouts and cancellations.

### The Cache Loading process
In a nutshell, a groupcache lookup of **Get("foo")** looks like:

(On machine #5 of a set of N machines running the same code)

1. Is the value of FOO in local cache because it's super hot?  If so, use it.
2. Is the value of FOO in local memory because peer #5 (the current
   peer) is the owner of it?  If so, use it.
3. Amongst all the peers in my set of N, am I the owner of the key
   FOO?  (e.g. does it consistent hash to 5?)  If so, load it and
   store in the local cache.  If other callers come in, via the same
   process or via RPC requests from peers, they block waiting for the load
   to finish and get the same answer.  If not, RPC to the peer that's the
   owner and get the answer.  If the RPC fails, just load it locally (still with
   local dup suppression).

<img src="docs/sequence-diagram.svg" alt="Simplified Diagram" width="800" /><br />

# Integrating GroupCache with HTTP Services
This is a quick guide on how to use groupcache in a service that is already listening for HTTP requests. In some
circumstances you may want to have groupcache respond using the same HTTP port that non groupcache requests are
received through. In this case you must explicitly create the `transport.HttpTransport` which can then be passed
to your HTTP router/handler.

```go
func main() {
    mux := http.NewServeMux()

    // Add endpoints specific to our application
    mux.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello, this is a non groupcache handler")
    })

    // Explicitly instantiate and use the HTTP transport
    t := transport.NewHttpTransport(
        transport.HttpTransportOptions{
        // BasePath specifies the HTTP path that will serve groupcache requests.
        // If blank, it defaults to "/_groupcache/".
        BasePath: "/_groupcache/",
        // Context optionally specifies a context for the server to use when it
        // receives a request.
        Context: nil,
        // Client optionally provide a custom http client with TLS config
        Client: nil,
        // Scheme is is either `http` or `https` defaults to `http`
        Scheme: "http",
        },
    )

    // Create a new groupcache instance
    instance := groupcache.New(groupcache.Options{
        // All of these fields are optional
        HashFn:    fnv1.HashBytes64,
        Logger:    slog.Default(),
        Transport: t,
        Replicas:  50,
    })

    // Add the groupcache handler
    mux.Handle("/_groupcache/", t)

    server := http.Server{
        Addr:    "192.168.1.1:8080",
        Handler: mux,
    }

    // Start a HTTP server to listen for peer requests from the groupcache
    go func() {
        log.Printf("Serving....\n")
        if err := server.ListenAndServe(); err != nil {
            log.Fatal(err)
        }
    }()
    defer func() { _ = server.Shutdown(context.Background()) }()
}
```

# Comparing Groupcache to memcached

### **Like memcached**, groupcache:
* shards by key to select which peer is responsible for that key

### **Unlike memcached**, groupcache:
* does not require running a separate set of servers, thus massively
  reducing deployment/configuration pain.  groupcache is a client
  library as well as a server.  It connects to its own peers.
* comes with a cache filling mechanism.  Whereas memcached just says
  "Sorry, cache miss", often resulting in a thundering herd of
  database (or whatever) loads from an unbounded number of clients
  (which has resulted in several fun outages), groupcache coordinates
  cache fills such that only one load in one process of an entire
  replicated set of processes populates the cache, then multiplexes
  the loaded value to all callers.
* does not support versioned values.  If key "foo" is value "bar",
  key "foo" must always be "bar".

# Pluggable Internal Cache
GroupCache supports replacing the default LRU cache implementation with alternative implementations.

[Otter](https://maypok86.github.io/otter/) is a high performance lockless cache suitable for high concurrency environments
where lock contention is an issue. Typically, servers with over 40 CPUs and lots of concurrent requests would benefit
from using an alternate cache implementation like Otter.

```go
import "github.com/groupcache/groupcache-go/v3/contrib"

// Create a new groupcache instance with a custom cache implementation
instance := groupcache.New(groupcache.Options{
    CacheFactory: func(maxBytes int64) (groupcache.Cache, error) {
        return contrib.NewOtterCache(maxBytes)
    },
    HashFn:    fnv1.HashBytes64,
    Logger:    slog.Default(),
    Transport: t,
    Replicas:  50,
})
```

#### Cache Size Implications for Otter
Due to the algorithm Otter uses to evict and track cache item costs, it is recommended to
use a larger maximum byte size when creating Groups via `Instance.NewGroup()` if you expect
your cached items to be very large. This is because groupcache uses a "Main Cache" and a
"Hot Cache" system where the "Hot Cache" is 1/8th the size of the maximum bytes requested.

Because Otter cache may reject items added to the cache which are larger than 1/10th of the
total capacity of the "Hot Cache" this may result in a lower hit rate for the "Hot Cache" when
storing large cache items and will penalize the efficiency of groupcache operation.

For example, If you expect the average item in cache to be 100 bytes, and you create a Group with a cache size
of 100,000 bytes, then the main cache will be 87,500 bytes and the hot cache will be 12,500 bytes.
Since the largest possible item in otter cache is 1/10th of the total size of the cache. Then the
largest item that could possibly fit into the hot cache is 1,250 bytes. If you think any of the
items you store in groupcache could be larger than 1,250 bytes. Then you should increase the maximum
bytes in a Group to accommodate the maximum cache item. If you have no estimate of the maximum size
of items in the groupcache, then you should monitor the `Cache.Stats().Rejected` stat for the cache
in production and adjust the size accordingly.


# Source Code Internals
If you are reading this, you are likely in front of a Github page and are interested in building a custom transport
or creating a Pull Request. In which case, the following explains the most of the important structs and how they 
interact with each other.

### Modifications from original library
The original author of groupcache is [Brad Fitzpatrick](https://github.com/bradfitz) who is also the
author of [memcached](https://memcached.org/). The original code repository for groupcache can be
found [here](https://github.com/golang/groupcache) and appears to be abandoned. We have taken the liberty
of modifying the library with additional features and fixing some deficiencies.

* Support for explicit key removal from a group. `Remove()` requests are
  first sent to the peer who owns the key, then the remove request is
  forwarded to every peer in the groupcache. NOTE: This is a best case design
  since it is possible a temporary network disruption could occur resulting
  in remove requests never making it their peers. In practice this scenario
  is very rare and the system remains very consistent. In case of an
  inconsistency placing a expiration time on your values will ensure the
  cluster eventually becomes consistent again.
* Support for expired values. `SetBytes()`, `SetProto()` and `SetString()` now
  accept an optional `time.Time` which represents a time in the future when the
  value will expire. If you don't want expiration, pass the zero value for
  `time.Time` (for instance, `time.Time{}`). Expiration is handled by the LRU Cache
  when a `Get()` on a key is requested. This means no network coordination of
  expired values is needed. However, this DOES require that the clock on all nodes in the
  cluster are synchronized for consistent expiration of values.
* Now always populating the hotcache. A more complex algorithm is unnecessary
  when the LRU cache will ensure the most used values remain in the cache. The
  evict code ensures the hotcache never overcrowds the maincache.
* Removed global state present in the original library to allow multiple groupcache
  instances to exist in code simultaneously.
* Separated the HTTP transport code from the rest of the code base such that third-party
  transports can be used without needing access to the internals of the library.
* Updated dependencies and use modern golang programming and documentation practices
* Added support for optional internal cache implementations.
* Many other code improvements

### groupcache.Instance
Represents an instance of groupcache. With the instance, you can create new groups and add other instances to your 
cluster by calling `Instance.SetPeers()`. Each instance communicates with other peers through the transport that is passed in
during creation. The`Instance.SetPeers()` calls `Transport.NewClient()` for each `peer.Info` struct provided to `SetPeers()`.
It is up to the transport implementation to create a client which is appropriate for communicating with the peer described by
the provided `peer.Info` struct.

It is up to the caller to ensure `Instance.SetPeers()` is called with a valid set of peers. Callers may want to use
a peer discovery mechanism to discover and update when the peer topology changes. `SetPeers()` is designed to be
called at any point during `groupcache.Instance` operation as peers leave or join the cluster.

If `SetPeers()` is not called, then the `groupcache.Instance` will operate as a local only cache.

### groupcache.Daemon
This is a convenience struct which encapsulates a `groupcache.Instance` to simplify starting and stopping an instance and
the associated transport. Calling `groupcache.ListenAndServe()` calls `Transport.ListenAndServe()` on the provided transport to
listen for incoming requests.

### groupcache.Group
Holds the cache that makes up the "group" which can be shared with other instances of group cache. Each
`groupcache.Instance` must create the same group using the same group name. Group names are how a "group" cache is
accessed by other peers in the cluster.

### groupcache.Cache
Is the cache implementation used for both the "hot" and "main" caches. The "main cache" stores items the local
group "owns" and the "hot cache" stores items this instance has retrieved from a remote peer. The "main cache" is
7/8th the size of the max bytes requested when calling `Instance.NewGroup()`. The "hot cache" is 1/8th the size of 
the "main cache".

Third party cache implementations can be used by providing an `Options.CacheFactory` function which returns the
third party initialized cache of the requested size. Groupcache includes an optional 
[Otter](https://maypok86.github.io/otter/) cache implementation which provides high concurrency performance
improvements. See the Otter section for details on usage.

### transport.Transport
Is an interface which is used to communicate with other peers in the cluster. The groupcache project provides 
`transport.HttpTransport` which is used by groupcache when no other custom transport is provided. Custom transports
must implement the `transport.Transport` and `peer.Client` interfaces. The `transport.Transport` implementation can
then be passed into the `groupcache.New()` method to register the transport. The `peer.Client` implementation is used
by `groupcache.Instance` and `peer.Picker` to communicate with other `groupcache.Instance` in the cluster using the
server started by the transport when `Transport.ListenAndServe()` is called. It is the responsibility of the caller to 
ensure `Transport.ListenAndServe()` is called successfully, else the `groupcache.Instance` will not be able to receive
any remote calls from peers in the cluster.

### transport.Sink
Sink is a collection of functions and structs which marshall and unmarshall strings, []bytes, and protobuf structs
for use in transporting data from one instance to another.

### peer.Picker
Is a consistent hash ring which holds an instantiated client for each peer in the cluster. It is used by   
`groupcache.Instance` to choose which peer in the cluster owns a key in the selected "group" cache.

### peer.Info
Is a struct which holds information used to identify each peer in the cluster. The `peer.Info` struct which represents
the current instance MUST be correctly identified by setting `IsSelf = true`. Without this, groupcache would send its 
self hash ring requests via the transport. To avoid accidentally creating a cluster without correctly identifying
which peer in the cluster is our instance, `Instance.SetPeers()` will return an error if at least one peer with
`IsSelf` is not set to `true`.

### cluster package
Is a convenience package containing functions to easily create and shutdown a cluster of groupcache instances 
(called daemons).

**Start()** and **StartWith()** starts a local cluster of groupcache daemons suitable for testing. Users who wish to
test groupcache in their own project test suites can use these methods to start and stop clusters.
See `cluster_test.go` for more examples.
```go
// Start a 3 instance cluster using the default options
_ := cluster.Start(context.Background(), 3, groupcache.Options{})
defer cluster.Shutdown(context.Background())
```

### Code Map
![docs/code-diagram.png](docs/code-diagram.png)
