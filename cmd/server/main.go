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

// main package is intended for testing the interaction between instances. Calls to `/set`
// will ONLY set the value for the running instance and NOT the groupcache. Values are stored
// in a global `store` and NOT in the groupcache group until they are fetched by a call to `/cache`.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

var store = map[string]string{}

func main() {
	addrFlag := flag.String("addr", "localhost:8080", "address:port to bind to")
	peerFlag := flag.String("peers", "localhost:8080", "a comma-separated list of groupcache peers")
	flag.Parse()

	var peers []peer.Info
	for _, p := range strings.Split(*peerFlag, ",") {
		peers = append(peers, peer.Info{
			IsSelf:  p == *addrFlag,
			Address: p,
		})
	}

	t := transport.NewHttpTransport(transport.HttpTransportOptions{})
	i := groupcache.New(groupcache.Options{
		Logger:    slog.Default(),
		Transport: t,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	err := i.SetPeers(ctx, peers)
	if err != nil {
		log.Fatalf(err.Error())
	}

	group, err := i.NewGroup("cache1", 64<<20, groupcache.GetterFunc(
		func(ctx context.Context, key string, dest transport.Sink) error {
			v, ok := store[key]
			if !ok {
				return fmt.Errorf("key not set")
			} else {
				if err := dest.SetBytes([]byte(v), time.Now().Add(10*time.Minute)); err != nil {
					log.Printf("Failed to set cache value for key '%s' - %v\n", key, err)
					return err
				}
			}

			return nil
		},
	))
	if err != nil {
		log.Fatalf(err.Error())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/set", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		key := r.FormValue("key")
		value := r.FormValue("value")
		fmt.Printf("Set: [%s]%s\n", key, value)
		store[key] = value
	})

	mux.HandleFunc("/cache", func(w http.ResponseWriter, r *http.Request) {
		key := r.FormValue("key")

		fmt.Printf("Fetching value for key '%s'\n", key)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var b []byte
		err := group.Get(ctx, key, transport.AllocatingByteSliceSink(&b))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
		_, _ = w.Write([]byte{'\n'})
	})
	mux.Handle(transport.DefaultBasePath, t)

	server := http.Server{
		Addr:    *addrFlag,
		Handler: mux,
	}

	go func() {
		log.Printf("Listening on '%s' for API requests", *addrFlag)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start HTTP server - %v", err)
		}
	}()

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)
	<-termChan
}
