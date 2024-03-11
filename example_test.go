package groupcache_test

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/groupcache/groupcache-go/v2"
	"github.com/groupcache/groupcache-go/v2/daemon"
	"github.com/groupcache/groupcache-go/v2/discovery"
	"github.com/segmentio/fasthash/fnv1"
)

// ExampleNew demonstrates starting a groupcache http instance with its own
// listener.
func ExampleNew() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)

	// Starts an HTTP listener with HTTP as the transport
	d, err := daemon.Spawn(ctx, daemon.Config{
		ListenAddress: "192.168.1.1:8080",
		Config: groupcache.Config{
			// These are Optional, with reasonable defaults
			PeerDiscovery: discovery.NewK8s(),
			Transport:     nil, // daemon.Spawn() ignores this if set.
			HashFn:        fnv1.HashBytes64,
			InstanceID:    "instance-01",
			Logger:        slog.Default(),
			Replicas:      50,
		},
	})
	cancel()
	if err != nil {
		log.Fatal("while starting server on 192.168.1.1:8080")
	}

	// Create a new group cache with a max cache size of 3MB
	group := d.NewGroup("users", 3000000, groupcache.GetterFunc(
		func(ctx context.Context, id string, dest groupcache.Sink) error {

			// In a real scenario we might fetch the value from a database.
			/*if user, err := fetchUserFromMongo(ctx, id); err != nil {
				return err
			}*/

			user := User{
				Id:      "12345",
				Name:    "John Doe",
				Age:     40,
				IsSuper: true,
			}

			// Set the user in the groupcache to expire after 5 minutes
			if err := dest.SetProto(&user, time.Now().Add(time.Minute*5)); err != nil {
				return err
			}
			return nil
		},
	))

	var user User

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := group.Get(ctx, "12345", groupcache.ProtoSink(&user)); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("-- User --\n")
	fmt.Printf("Id: %s\n", user.Id)
	fmt.Printf("Name: %s\n", user.Name)
	fmt.Printf("Age: %d\n", user.Age)
	fmt.Printf("IsSuper: %t\n", user.IsSuper)

	// Remove the key from the groupcache
	if err := group.Remove(ctx, "12345"); err != nil {
		fmt.Printf("Remove Err: %s\n", err)
		log.Fatal(err)
	}

	// Shutdown the daemon
	d.Shutdown(context.Background())
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
	transport := groupcache.NewHttpTransport(
		groupcache.HttpTransportConfig{
			BasePath: "/_groupcache/",
			// Optional
			HTTPTransport: nil,
			HTTPContext:   nil,
		},
	)

	// Use a static peer list instead of dynamic peer discovery
	static := discovery.NewStatic([]discovery.Peer{
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

	// Create a new groupcache instance
	instance := groupcache.New(groupcache.Config{
		PeerDiscovery: static,
		HashFn:        fnv1.HashBytes64,
		InstanceID:    "instance-01",
		Logger:        slog.Default(),
		Transport:     transport,
		Replicas:      50,
	})

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
	defer server.Shutdown(context.Background())

	// Update the static peer config while groupcache is running
	static.SetPeers([]discovery.Peer{
		{
			Address: "192.168.1.1:8080",
			IsSelf:  true,
		},
		{
			Address: "192.168.1.1:8081",
			IsSelf:  false,
		},
	})

	// Create a new group cache with a max cache size of 3MB
	group := instance.NewGroup("users", 3000000, groupcache.GetterFunc(
		func(ctx context.Context, id string, dest groupcache.Sink) error {

			// In a real scenario we might fetch the value from a database.
			/*if user, err := fetchUserFromMongo(ctx, id); err != nil {
				return err
			}*/

			user := User{
				Id:      "12345",
				Name:    "John Doe",
				Age:     40,
				IsSuper: true,
			}

			// Set the user in the groupcache to expire after 5 minutes
			if err := dest.SetProto(&user, time.Now().Add(time.Minute*5)); err != nil {
				return err
			}
			return nil
		},
	))

	var user User

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := group.Get(ctx, "12345", groupcache.ProtoSink(&user)); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("-- User --\n")
	fmt.Printf("Id: %s\n", user.Id)
	fmt.Printf("Name: %s\n", user.Name)
	fmt.Printf("Age: %d\n", user.Age)
	fmt.Printf("IsSuper: %t\n", user.IsSuper)

	// Remove the key from the groupcache
	if err := group.Remove(ctx, "12345"); err != nil {
		fmt.Printf("Remove Err: %s\n", err)
		log.Fatal(err)
	}

}
