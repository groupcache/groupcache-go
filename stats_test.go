package groupcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/groupcache/groupcache-go/v3/transport"
)

func TestNewMeterProviderUsesGlobalProviderByDefault(t *testing.T) {
	t.Parallel()

	original := otel.GetMeterProvider()
	global := &recordingMeterProvider{}
	otel.SetMeterProvider(global)
	defer otel.SetMeterProvider(original)

	mp := NewMeterProvider()

	require.Equal(t, global, mp.underlying)
	assert.Contains(t, global.requested, instrumentationName)
	assert.Same(t, global.meter, mp.getMeter())
}

func TestNewMeterProviderRespectsOverride(t *testing.T) {
	t.Parallel()

	original := otel.GetMeterProvider()
	defer otel.SetMeterProvider(original)

	global := &recordingMeterProvider{}
	otel.SetMeterProvider(global)
	initialGlobalRequests := len(global.requested)

	custom := &recordingMeterProvider{}
	mp := NewMeterProvider(WithMeterProvider(custom))

	require.Equal(t, custom, mp.underlying)
	assert.Contains(t, custom.requested, instrumentationName)
	assert.Equal(t, initialGlobalRequests, len(global.requested), "global provider should not be invoked when override is provided")
	assert.Same(t, custom.meter, mp.getMeter())
}

func TestNewGroupInstrumentsRegistersAllCounters(t *testing.T) {
	t.Parallel()

	meter := &recordingMeter{}

	inst, err := newGroupInstruments(meter)
	require.NoError(t, err)
	require.NotNil(t, inst)

	expectedCounters := []string{
		"groupcache.group.gets",
		"groupcache.group.cache_hits",
		"groupcache.group.peer.loads",
		"groupcache.group.peer.errors",
		"groupcache.group.loads",
		"groupcache.group.loads.deduped",
		"groupcache.group.local.loads",
		"groupcache.group.local.load_errors",
		"groupcache.group.batch.removes",
		"groupcache.group.batch.keys_removed",
	}
	assert.Equal(t, expectedCounters, meter.counterNames)
	assert.Equal(t, []string{"groupcache.group.peer.latency_max_ms"}, meter.updownNames)

	assert.NotNil(t, inst.GetsCounter())
	assert.NotNil(t, inst.HitsCounter())
	assert.NotNil(t, inst.PeerLoadsCounter())
	assert.NotNil(t, inst.PeerErrorsCounter())
	assert.NotNil(t, inst.LoadsCounter())
	assert.NotNil(t, inst.LoadsDedupedCounter())
	assert.NotNil(t, inst.LocalLoadsCounter())
	assert.NotNil(t, inst.LocalLoadErrsCounter())
	assert.NotNil(t, inst.GetFromPeersLatencyMaxGauge())
	assert.NotNil(t, inst.BatchRemovesCounter())
	assert.NotNil(t, inst.BatchKeysRemovedCounter())
}

func TestNewGroupInstrumentsErrorsOnCounterFailure(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("counter fail")
	meter := &failingObservableMeter{counterErr: expectedErr}

	inst, err := newGroupInstruments(meter)
	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, inst)
}

func TestNewGroupInstrumentsErrorsOnUpDownCounterFailure(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("updown fail")
	meter := &failingObservableMeter{upDownErr: expectedErr}

	inst, err := newGroupInstruments(meter)
	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, inst)
}

func TestNewCacheInstrumentsErrorsOnCounterFailure(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("counter fail")
	meter := &failingSyncMeter{counterErr: expectedErr}

	inst, err := newCacheInstruments(meter)
	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, inst)
}

func TestNewCacheInstrumentsErrorsOnUpDownCounterFailure(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("updown fail")
	meter := &failingSyncMeter{upDownErr: expectedErr}

	inst, err := newCacheInstruments(meter)
	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, inst)
}

func TestNewGroupPropagatesMetricRegistrationError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("register fail")
	failMeter := &failingObservableMeter{counterErr: expectedErr}
	mp := NewMeterProvider(WithMeterProvider(&staticMeterProvider{meter: failMeter}))

	instance := New(Options{MetricProvider: mp})

	g, err := instance.NewGroup("metrics-error", 1<<10, GetterFunc(func(_ context.Context, key string, dest transport.Sink) error {
		return dest.SetString("ok", time.Time{})
	}))

	require.ErrorIs(t, err, expectedErr)
	assert.Nil(t, g)
}

func TestNewCacheInstrumentsRegistersAllCounters(t *testing.T) {
	t.Parallel()

	meter := &recordingSyncMeter{}

	inst, err := newCacheInstruments(meter)
	require.NoError(t, err)
	require.NotNil(t, inst)

	expectedCounters := []string{
		"groupcache.cache.rejected",
		"groupcache.cache.gets",
		"groupcache.cache.hits",
		"groupcache.cache.evictions",
	}
	assert.Equal(t, expectedCounters, meter.counterNames)
	assert.Equal(t, []string{
		"groupcache.cache.bytes",
		"groupcache.cache.items",
	}, meter.updownNames)

	assert.NotNil(t, inst.RejectedCounter())
	assert.NotNil(t, inst.BytesGauge())
	assert.NotNil(t, inst.ItemsGauge())
	assert.NotNil(t, inst.GetsCounter())
	assert.NotNil(t, inst.HitsCounter())
	assert.NotNil(t, inst.EvictionsCounter())
}

type recordingMeterProvider struct {
	noop.MeterProvider

	requested []string
	meter     *recordingMeter
}

func (p *recordingMeterProvider) Meter(name string, _ ...metric.MeterOption) metric.Meter {
	p.requested = append(p.requested, name)
	if p.meter == nil {
		p.meter = &recordingMeter{}
	}
	return p.meter
}

type recordingMeter struct {
	noop.Meter

	counterNames []string
	updownNames  []string
}

func (m *recordingMeter) Int64ObservableCounter(name string, _ ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	m.counterNames = append(m.counterNames, name)
	return noop.Int64ObservableCounter{}, nil
}

func (m *recordingMeter) Int64ObservableUpDownCounter(name string, _ ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	m.updownNames = append(m.updownNames, name)
	return noop.Int64ObservableUpDownCounter{}, nil
}

type failingObservableMeter struct {
	noop.Meter

	counterErr error
	upDownErr  error
}

func (m *failingObservableMeter) Int64ObservableCounter(string, ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}
	return noop.Int64ObservableCounter{}, nil
}

func (m *failingObservableMeter) Int64ObservableUpDownCounter(string, ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	if m.upDownErr != nil {
		return nil, m.upDownErr
	}
	return noop.Int64ObservableUpDownCounter{}, nil
}

type failingSyncMeter struct {
	noop.Meter

	counterErr error
	upDownErr  error
}

func (m *failingSyncMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}
	return noop.Int64Counter{}, nil
}

func (m *failingSyncMeter) Int64UpDownCounter(string, ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	if m.upDownErr != nil {
		return nil, m.upDownErr
	}
	return noop.Int64UpDownCounter{}, nil
}

type staticMeterProvider struct {
	noop.MeterProvider
	meter metric.Meter
}

func (s *staticMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return s.meter
}

type recordingSyncMeter struct {
	noop.Meter

	counterNames []string
	updownNames  []string
}

func (m *recordingSyncMeter) Int64Counter(name string, _ ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	m.counterNames = append(m.counterNames, name)
	return noop.Int64Counter{}, nil
}

func (m *recordingSyncMeter) Int64UpDownCounter(name string, _ ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	m.updownNames = append(m.updownNames, name)
	return noop.Int64UpDownCounter{}, nil
}
