/*
Copyright 2024 Derrick J Wippler

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

package transport_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	"github.com/groupcache/groupcache-go/v3"
	"github.com/groupcache/groupcache-go/v3/cluster"
	"github.com/groupcache/groupcache-go/v3/transport"
	"github.com/groupcache/groupcache-go/v3/transport/pb"
	"github.com/groupcache/groupcache-go/v3/transport/peer"
)

const groupName = "group-a"

func TestHttpTransport(t *testing.T) {
	// Start a http server to count the number of non cached hits
	var serverHits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "Hello")
		serverHits++
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// ListenAndServe a cluster of 4 groupcache instances with HTTP Transport
	require.NoError(t, cluster.Start(ctx, 4, groupcache.Options{
		Transport: transport.NewHttpTransport(transport.HttpTransportOptions{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}),
	}))
	defer func() { _ = cluster.Shutdown(context.Background()) }()

	// Create a group on each instance in the cluster
	for idx, d := range cluster.ListDaemons() {
		_, err := d.GetInstance().NewGroup(groupName, 1<<20,
			groupcache.GetterFunc(func(ctx context.Context, key string, dest transport.Sink) error {
				if _, err := http.Get(ts.URL); err != nil {
					t.Logf("HTTP request from getter failed with '%s'", err)
				}
				return dest.SetString(strconv.Itoa(idx)+":"+key, time.Time{})
			}))
		require.NoError(t, err)
	}

	// Create new transport with default options
	tr := transport.NewHttpTransport(transport.HttpTransportOptions{})

	// Create a new client to the first peer in the cluster
	c, err := tr.NewClient(ctx, cluster.PeerAt(0))
	require.NoError(t, err)

	// Each new key should result in a new hit to the test server
	for _, key := range testKeys(100) {
		var resp pb.GetResponse
		require.NoError(t, getRequest(ctx, c, groupName, key, &resp))

		// The value should be in the format `instance:key`
		assert.True(t, strings.HasSuffix(string(resp.Value), ":"+key))
	}
	assert.Equal(t, 100, serverHits)

	serverHits = 0

	// Multiple gets on the same key to the owner of the key
	owner := cluster.FindOwningDaemon("new-key")
	for i := 0; i < 2; i++ {
		var resp pb.GetResponse
		require.NoError(t, getRequest(ctx, owner.MustClient(), groupName, "new-key", &resp))
	}
	// Should result in only 1 server get
	assert.Equal(t, 1, serverHits)

	// Remove the key from the owner and we should see another server hit
	var resp pb.GetResponse
	require.NoError(t, removeRequest(ctx, owner.MustClient(), groupName, "new-key"))
	require.NoError(t, getRequest(ctx, owner.MustClient(), groupName, "new-key", &resp))
	assert.Equal(t, 2, serverHits)

	// Remove the key, and set it with a different value
	require.NoError(t, removeRequest(ctx, owner.MustClient(), groupName, "new-key"))
	require.NoError(t, setRequest(ctx, owner.MustClient(), groupName, "new-key", "new-value"))

	require.NoError(t, getRequest(ctx, owner.MustClient(), groupName, "new-key", &resp))
	assert.Equal(t, []byte("new-value"), resp.Value)
	// Should not see any new server hits
	assert.Equal(t, 2, serverHits)
}

func getRequest(ctx context.Context, c peer.Client, group, key string, resp *pb.GetResponse) error {
	req := &pb.GetRequest{
		Group: &group,
		Key:   &key,
	}
	return c.Get(ctx, req, resp)
}

func setRequest(ctx context.Context, c peer.Client, group, key, value string) error {
	req := &pb.SetRequest{
		Value: []byte(value),
		Group: &group,
		Key:   &key,
	}
	return c.Set(ctx, req)
}

func removeRequest(ctx context.Context, c peer.Client, group, key string) error {
	req := &pb.GetRequest{
		Group: &group,
		Key:   &key,
	}
	return c.Remove(ctx, req)
}

func testKeys(n int) (keys []string) {
	keys = make([]string, n)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	return
}

// nolint
func TestHttpTransportWithTracerEmitsServerSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	tr := transport.NewHttpTransport(transport.HttpTransportOptions{
		Tracer: transport.NewTracer(transport.WithTraceProvider(tracerProvider)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	instance := newTracingInstance()
	tr.Register(instance)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	require.NoError(t, tr.ListenAndServe(ctx, "127.0.0.1:0"))
	t.Cleanup(func() { _ = tr.Shutdown(context.Background()) })

	resp, err := http.Get(fmt.Sprintf("http://%s%s%s/%s", tr.ListenAddress(), transport.DefaultBasePath, groupName, "tracer"))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	spans := recorder.Ended()
	require.NotEmpty(t, spans, "expected otelhttp handler to record a span when tracer is configured")

	var serverSpanFound bool
	for _, span := range spans {
		if span.SpanKind() == trace.SpanKindServer {
			serverSpanFound = true
			break
		}
	}
	assert.True(t, serverSpanFound, "expected a server span recorded by the tracer provider")
}

// nolint
func TestHttpClientWithTracerRecordsSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := transport.NewTracer(
		transport.WithTraceProvider(tracerProvider),
		transport.WithTracerAttributes(attribute.String("component", "http-client")),
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "fail-get"):
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case r.Method == http.MethodGet:
			body, _ := proto.Marshal(&pb.GetResponse{Value: []byte("value")})
			_, _ = w.Write(body)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "fail-set"):
			http.Error(w, "cannot set", http.StatusInternalServerError)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "fail-remove"):
			http.Error(w, "cannot delete", http.StatusInternalServerError)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	tr := transport.NewHttpTransport(transport.HttpTransportOptions{
		Tracer: tracer,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	client, err := tr.NewClient(context.Background(), peer.Info{Address: serverURL.Host})
	require.NoError(t, err)

	ctx := context.Background()

	require.NoError(t, setRequest(ctx, client, groupName, "ok-set", "value"))

	var resp pb.GetResponse
	require.NoError(t, getRequest(ctx, client, groupName, "ok-get", &resp))
	require.NoError(t, removeRequest(ctx, client, groupName, "ok-remove"))

	require.Error(t, getRequest(ctx, client, groupName, "fail-get", &resp))
	require.Error(t, setRequest(ctx, client, groupName, "fail-set", "value"))
	require.Error(t, removeRequest(ctx, client, groupName, "fail-remove"))

	spans := recorder.Ended()
	clientSpans := spansWithPrefix(spans, "GroupCache.")
	require.Len(t, clientSpans, 6)

	expected := []struct {
		name   string
		status codes.Code
	}{
		{name: "GroupCache.Set", status: codes.Ok},
		{name: "GroupCache.Get", status: codes.Ok},
		{name: "GroupCache.Remove", status: codes.Ok},
		{name: "GroupCache.Get", status: codes.Error},
		{name: "GroupCache.Set", status: codes.Error},
		{name: "GroupCache.Remove", status: codes.Error},
	}

	for idx, exp := range expected {
		require.Less(t, idx, len(clientSpans))
		assert.Equal(t, exp.name, clientSpans[idx].Name())
		assert.Equal(t, exp.status, clientSpans[idx].Status().Code)
		assert.True(t, spanHasAttribute(clientSpans[idx], attribute.Key("component"), "http-client"))
	}
}

type tracingInstance struct {
	group *tracingGroup
}

func newTracingInstance() *tracingInstance {
	return &tracingInstance{group: newTracingGroup()}
}

func (t *tracingInstance) GetGroup(_ string) transport.Group {
	return t.group
}

type tracingGroup struct {
	values map[string][]byte
	mu     sync.Mutex
}

func newTracingGroup() *tracingGroup {
	return &tracingGroup{
		values: make(map[string][]byte),
	}
}

func (t *tracingGroup) Set(_ context.Context, key string, value []byte, _ time.Time, _ bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.values[key] = append([]byte(nil), value...)
	return nil
}

func (t *tracingGroup) RemoteSet(key string, value []byte, expire time.Time) {
	_ = t.Set(context.Background(), key, value, expire, false)
}

func (t *tracingGroup) Get(_ context.Context, key string, dest transport.Sink) error {
	t.mu.Lock()
	value, ok := t.values[key]
	t.mu.Unlock()

	if !ok {
		value = []byte("value:" + key)
	}
	return dest.SetBytes(value, time.Time{})
}

func (t *tracingGroup) Remove(_ context.Context, key string) error {
	t.LocalRemove(key)
	return nil
}

func (t *tracingGroup) LocalRemove(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.values, key)
}

func (t *tracingGroup) UsedBytes() (int64, int64) {
	return 0, 0
}

func (t *tracingGroup) Name() string {
	return groupName
}

func spanHasAttribute(span sdktrace.ReadOnlySpan, key attribute.Key, expected string) bool {
	for _, attr := range span.Attributes() {
		if attr.Key == key && attr.Value.AsString() == expected {
			return true
		}
	}
	return false
}

func spansWithPrefix(spans []sdktrace.ReadOnlySpan, prefix string) []sdktrace.ReadOnlySpan {
	filtered := make([]sdktrace.ReadOnlySpan, 0, len(spans))
	for _, span := range spans {
		if strings.HasPrefix(span.Name(), prefix) {
			filtered = append(filtered, span)
		}
	}
	return filtered
}
