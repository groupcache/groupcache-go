/*
Copyright Derrick J Wippler
Copyright Arsene Tochemey Gandote

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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type recorderTracerProvider struct {
	trace.TracerProvider
	mu        sync.Mutex
	requested []string
}

func (r *recorderTracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	r.mu.Lock()
	r.requested = append(r.requested, name)
	r.mu.Unlock()
	return r.TracerProvider.Tracer(name, opts...)
}

func newRecorderTracerProvider() *recorderTracerProvider {
	return &recorderTracerProvider{TracerProvider: noop.NewTracerProvider()}
}

// nolint
func TestNewTracerUsesGlobalProviderWhenNoneProvided(t *testing.T) {
	t.Parallel()

	original := otel.GetTracerProvider()
	rec := newRecorderTracerProvider()
	otel.SetTracerProvider(rec)
	defer otel.SetTracerProvider(original)

	tracer := NewTracer()

	require.Equal(t, rec, tracer.traceProvider)
	require.Equal(t, []string{instrumentationName}, rec.requested)
	require.NotNil(t, tracer.getTracer())
}

// nolint
func TestNewTracerRespectsCustomProviderOption(t *testing.T) {
	t.Parallel()

	original := otel.GetTracerProvider()
	defer otel.SetTracerProvider(original)

	// Set a global provider that should not be used after the override.
	global := newRecorderTracerProvider()
	otel.SetTracerProvider(global)

	custom := newRecorderTracerProvider()
	tracer := NewTracer(WithTraceProvider(custom))

	require.Equal(t, custom, tracer.traceProvider)
	require.Equal(t, []string{instrumentationName}, custom.requested)
	require.Empty(t, global.requested)
}

// nolint
func TestWithTracerAttributesAppendsAttributes(t *testing.T) {
	t.Parallel()

	attrs := []attribute.KeyValue{
		attribute.String("env", "test"),
		attribute.Int("shard", 1),
	}

	tracer := NewTracer(WithTracerAttributes(attrs...))
	require.Equal(t, attrs, tracer.traceAttributes)
}

// nolint
func TestSpanStartOptionsPoolResetsOnPut(t *testing.T) {
	t.Parallel()

	tracer := NewTracer()

	opts := tracer.getSpanStartOptions()
	require.Zero(t, len(*opts))
	require.GreaterOrEqual(t, cap(*opts), 10)

	*opts = append(*opts, trace.WithSpanKind(trace.SpanKindClient))
	require.Equal(t, 1, len(*opts))

	tracer.putSpanStartOptions(opts)

	opts = tracer.getSpanStartOptions()
	require.Zero(t, len(*opts))
	require.GreaterOrEqual(t, cap(*opts), 10)
	tracer.putSpanStartOptions(opts)
}

// nolint
func TestAttributesPoolResetsOnPut(t *testing.T) {
	t.Parallel()

	tracer := NewTracer()

	attrs := tracer.getAttributes()
	require.Zero(t, len(*attrs))
	require.GreaterOrEqual(t, cap(*attrs), 10)

	*attrs = append(*attrs, attribute.String("key", "val"))
	require.Equal(t, 1, len(*attrs))

	tracer.putAttributes(attrs)

	attrs = tracer.getAttributes()
	require.Zero(t, len(*attrs))
	require.GreaterOrEqual(t, cap(*attrs), 10)
	tracer.putAttributes(attrs)
}
