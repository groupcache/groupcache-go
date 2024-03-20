package groupcache_test

import (
	"context"
	"fmt"
	"github.com/groupcache/groupcache-go/v3"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/groupcache/groupcache-go/v3/cluster"
	"github.com/groupcache/groupcache-go/v3/data"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
	"github.com/segmentio/fasthash/fnv1"
)

// ExampleNew demonstrates starting a groupcache http instance with its own
// listener.
func ExampleNew() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)

	// Starts an instance of groupcache with the provided transport
	d, err := cluster.SpawnDaemon(ctx, "192.168.1.1:8080", groupcache.Options{
		// If transport is nil, defaults to HttpTransport
		Transport: nil,
		HashFn:    fnv1.HashBytes64,
		Logger:    slog.Default(),
		Replicas:  50,
	})
	cancel()
	if err != nil {
		log.Fatal("while starting server on 192.168.1.1:8080")
	}

	// Create a new group cache with a max cache size of 3MB
	group, err := d.GroupCache.NewGroup("users", 3000000, groupcache.GetterFunc(
		func(ctx context.Context, id string, dest data.Sink) error {
			// Set the user in the groupcache to expire after 5 minutes
			if err := dest.SetString("hello", time.Now().Add(time.Minute*5)); err != nil {
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

	var value string
	if err := group.Get(ctx, "12345", data.StringSink(&value)); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Value: %s\n", value)

	// Remove the key from the groupcache
	if err := group.Remove(ctx, "12345"); err != nil {
		fmt.Printf("Remove Err: %s\n", err)
		log.Fatal(err)
	}

	// Shutdown the daemon
	_ = d.Shutdown(context.Background())
}

// ExampleNewHttpTransport demonstrates how to use groupcache in a service that
// is already listening for HTTP requests.
func ExampleNewHttpTransport() {
	mux := http.NewServeMux()

	// Add endpoints specific to our application
	mux.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, this is a non groupcache handler")
	})

	// Explicitly instantiate and use the HTTP transport
	transport := transport.NewHttpTransport(
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
			Scheme: "",
		},
	)

	// Create a new groupcache instance
	instance := groupcache.New(groupcache.Options{
		HashFn:    fnv1.HashBytes64,
		Logger:    slog.Default(),
		Transport: transport,
		Replicas:  50,
	})

	// You can set the peers manually
	err := instance.SetPeers(context.Background(), []peer.Info{
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
	if err != nil {
		log.Fatal(err)
	}
	// OR you can register a peer discovery mechanism
	//d := discovery.SpawnK8s(discovery.K8sConfig{
	//	OnUpdate: instance.SetPeers,
	//})
	//defer d.Shutdown(context.Background())

	// Add the groupcache handler
	mux.Handle("/_groupcache/", transport)

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

	// Update the static peer config while groupcache is running
	err = instance.SetPeers(context.Background(), []peer.Info{
		{
			Address: "192.168.1.1:8080",
			IsSelf:  true,
		},
		{
			Address: "192.168.1.1:8081",
			IsSelf:  false,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create a new group cache with a max cache size of 3MB
	group, err := instance.NewGroup("users", 3000000, groupcache.GetterFunc(
		func(ctx context.Context, id string, dest data.Sink) error {
			// Set the user in the groupcache to expire after 5 minutes
			if err := dest.SetString("hello", time.Now().Add(time.Minute*5)); err != nil {
				return err
			}
			return nil
		},
	))
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var value string
	if err := group.Get(ctx, "12345", data.StringSink(&value)); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Value: %s\n", value)

	// Remove the key from the groupcache
	if err := group.Remove(ctx, "12345"); err != nil {
		fmt.Printf("Remove Err: %s\n", err)
		log.Fatal(err)
	}

}
