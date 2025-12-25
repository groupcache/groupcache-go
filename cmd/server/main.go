// Package main implements a test server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modernprogram/groupcache/v2"
)

var store = map[string]string{}

const purgeExpired = true
const ttl = 10 * time.Second
const expiredKeysEvictionInterval = 20 * time.Second

var group = groupcache.NewGroupWithWorkspace(groupcache.Options{
	Workspace:       groupcache.DefaultWorkspace,
	Name:            "cache1",
	PurgeExpired:    purgeExpired,
	CacheBytesLimit: 64 << 20,
	Getter: groupcache.GetterFunc(
		func(ctx context.Context, key string, dest groupcache.Sink, info *groupcache.Info) error {

			var ctx1, ctx2 string
			if info != nil {
				// retrieves our optional user-supplied per-request context information.
				ctx1 = info.Ctx1
				ctx2 = info.Ctx2
			}

			fmt.Printf("Get Called - loading key=%s from primary source (ctx1:%s ctx2:%s)\n",
				key, ctx1, ctx2)
			v, ok := store[key]
			if !ok {
				return fmt.Errorf("key not set")
			}
			if err := dest.SetBytes([]byte(v), time.Now().Add(ttl)); err != nil {
				log.Printf("Failed to set cache value for key '%s' - %v\n", key, err)
				return err
			}
			return nil
		},
	),
	ExpiredKeysEvictionInterval: expiredKeysEvictionInterval,
})

func main() {
	serverURL := flag.String("server-url", "http://localhost:8080", "server url")
	addr := flag.String("addr", ":8080", "server address")
	addr2 := flag.String("api-addr", ":8081", "api server address")
	peers := flag.String("pool", *serverURL, "comma-separated server pool list")
	flag.Parse()

	p := strings.Split(*peers, ",")
	pool := groupcache.NewHTTPPoolOptsWithWorkspace(groupcache.DefaultWorkspace, *serverURL,
		&groupcache.HTTPPoolOptions{})
	pool.Set(p...)

	http.HandleFunc("/set", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := r.FormValue("key")
		value := r.FormValue("value")
		fmt.Printf("Set: [%s]%s\n", key, value)
		store[key] = value
	})

	http.HandleFunc("/cache", func(w http.ResponseWriter, r *http.Request) {
		key := r.FormValue("key")

		fmt.Printf("Fetching value for key '%s'\n", key)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var b []byte
		err := group.Get(ctx, key, groupcache.AllocatingByteSliceSink(&b), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
		_, _ = w.Write([]byte{'\n'})
	})

	http.HandleFunc("/cachewithcontext", func(w http.ResponseWriter, r *http.Request) {
		key := r.FormValue("key")

		fmt.Printf("Fetching value for key '%s'\n", key)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		info := &groupcache.Info{Ctx1: "ctxValue1", Ctx2: "ctxValue2"}

		var b []byte
		err := group.Get(ctx, key, groupcache.AllocatingByteSliceSink(&b), info)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
		_, _ = w.Write([]byte{'\n'})
	})

	server := http.Server{
		Addr:    *addr,
		Handler: pool,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start HTTP server - %v", err)
		}
	}()

	go func() {
		if err := http.ListenAndServe(*addr2, nil); err != nil {
			log.Fatalf("Failed to start API HTTP server - %v", err)
		}
	}()

	fmt.Println("Running...")
	fmt.Println()
	fmt.Printf("TTL: %v\n", ttl)
	fmt.Printf("expiredKeysEvictionInterval: %v\n", expiredKeysEvictionInterval)
	fmt.Println()
	fmt.Println("Try: curl -d key=key1 -d value=value1 localhost:8081/set")
	fmt.Println("Try: curl -d key=key1 localhost:8081/cache")
	fmt.Println("Try: curl -d key=key1 localhost:8081/cachewithcontext")
	fmt.Println()
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)
	<-termChan
}
