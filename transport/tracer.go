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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/groupcache/groupcache-go/instrumentation/otel"
)

type TracerOption func(*Tracer)

type Tracer struct {
	traceProvider        trace.TracerProvider
	tracer               trace.Tracer
	traceAttributes      []attribute.KeyValue
	spanStartOptionsPool sync.Pool
	attributesPool       sync.Pool
}

func WithTraceProvider(tp trace.TracerProvider) TracerOption {
	return func(t *Tracer) {
		if tp != nil {
			t.traceProvider = tp
		}
	}
}

func WithTracerAttributes(attrs ...attribute.KeyValue) TracerOption {
	return func(t *Tracer) {
		t.traceAttributes = append(t.traceAttributes, attrs...)
	}
}

func NewTracer(opts ...TracerOption) *Tracer {
	t := &Tracer{
		traceProvider: otel.GetTracerProvider(),
		spanStartOptionsPool: sync.Pool{
			New: func() any {
				s := make([]trace.SpanStartOption, 0, 10)
				return &s
			},
		},
		attributesPool: sync.Pool{
			New: func() any {
				s := make([]attribute.KeyValue, 0, 10)
				return &s
			},
		},
	}

	for _, opt := range opts {
		opt(t)
	}

	t.tracer = t.traceProvider.Tracer(instrumentationName)
	return t
}

func (t *Tracer) getTracer() trace.Tracer {
	return t.tracer
}

func (t *Tracer) getSpanStartOptions() *[]trace.SpanStartOption {
	return t.spanStartOptionsPool.Get().(*[]trace.SpanStartOption)
}

func (t *Tracer) putSpanStartOptions(opts *[]trace.SpanStartOption) {
	*opts = (*opts)[:0]
	t.spanStartOptionsPool.Put(opts)
}

func (t *Tracer) getAttributes() *[]attribute.KeyValue {
	return t.attributesPool.Get().(*[]attribute.KeyValue)
}

func (t *Tracer) putAttributes(attrs *[]attribute.KeyValue) {
	*attrs = (*attrs)[:0]
	t.attributesPool.Put(attrs)
}
