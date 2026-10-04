package metry

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/testutil"
)

type lifecycleTracer struct {
	trace.TracerProvider

	flushes   atomic.Int32
	shutdowns atomic.Int32
	flushErr  error
}

func (p *lifecycleTracer) ForceFlush(context.Context) error { p.flushes.Add(1); return p.flushErr }
func (p *lifecycleTracer) Shutdown(context.Context) error   { p.shutdowns.Add(1); return nil }

type lifecycleMeter struct {
	metric.MeterProvider

	flushes   atomic.Int32
	shutdowns atomic.Int32
}

func (p *lifecycleMeter) ForceFlush(context.Context) error { p.flushes.Add(1); return nil }
func (p *lifecycleMeter) Shutdown(context.Context) error   { p.shutdowns.Add(1); return nil }

type lifecycleMetricExporter struct {
	sdkmetric.Exporter

	shutdowns atomic.Int32
}

func (e *lifecycleMetricExporter) Shutdown(ctx context.Context) error {
	e.shutdowns.Add(1)
	return e.Exporter.Shutdown(ctx)
}

type lifecycleExporter struct {
	sdktrace.SpanExporter

	shutdowns atomic.Int32
}

func (e *lifecycleExporter) Shutdown(ctx context.Context) error {
	e.shutdowns.Add(1)
	return e.SpanExporter.Shutdown(ctx)
}

func TestFromSDK_BorrowedLifecycle(t *testing.T) {
	// Arrange.
	sdk := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = sdk.Shutdown(context.Background()) })
	host := &lifecycleTracer{TracerProvider: sdk}
	meter := &lifecycleMeter{MeterProvider: noop.NewMeterProvider()}
	deps := SDKDependencies{
		TracerProvider: host,
		MeterProvider:  meter,
		Propagator:     propagation.TraceContext{},
	}
	provider, err := FromSDK(deps)
	require.NoError(t, err)
	// Act.
	require.NoError(t, provider.ForceFlush(context.Background()))
	require.NoError(t, provider.Shutdown(context.Background()))
	require.NoError(t, provider.Shutdown(context.Background()))
	// Assert: no implicit activity on host-owned pipeline.
	require.Zero(t, host.flushes.Load())
	require.Zero(t, host.shutdowns.Load())
	require.Zero(t, meter.shutdowns.Load())
	require.Zero(t, meter.flushes.Load())
	require.NoError(t, provider.FlushDependencies(context.Background()))
	require.EqualValues(t, 1, host.flushes.Load())
	require.EqualValues(t, 1, meter.flushes.Load())
	snapshot := provider.SDK()
	snapshot.TracerProvider = nil
	require.Nil(t, snapshot.TracerProvider)
	require.Same(t, host, provider.SDK().TracerProvider)
	failure := errors.New("flush failed")
	host.flushErr = failure
	require.ErrorIs(t, provider.FlushDependencies(context.Background()), failure)
}

func TestNew_OwnershipAndInvalidInputs(t *testing.T) {
	// Arrange.
	exp := &lifecycleExporter{SpanExporter: testutil.NewInMemoryTraceExporter().SDKSpanExporter()}
	metricExp := &lifecycleMetricExporter{Exporter: testutil.NewInMemoryMetricExporter()}
	var typedNil *lifecycleExporter
	invalid := [][]Option{
		{nil}, {WithTraceRatio(math.NaN())}, {WithTraceRatio(math.Inf(1))},
		{WithTraceRatio(-1)}, {WithTraceRatio(2)}, {WithExporter(nil)},
		{WithExporter(typedNil)}, {WithMetricExporter(nil)}, {WithSampler(nil)},
	}
	// Act / Assert: failed creation never takes ownership.
	for _, opts := range invalid {
		options := append([]Option{WithServiceName("test"), WithExporter(exp), WithMetricExporter(metricExp)}, opts...)
		provider, err := New(context.Background(), options...)
		require.ErrorIs(t, err, ErrInvalidConfiguration)
		require.Nil(t, provider)
	}
	require.Zero(t, exp.shutdowns.Load())
	require.Zero(t, metricExp.shutdowns.Load())
	provider, err := New(
		context.Background(),
		WithServiceName("owned"),
		WithExporter(exp),
		WithMetricExporter(metricExp),
	)
	require.NoError(t, err)
	require.NoError(t, provider.Shutdown(context.Background()))
	require.NoError(t, provider.Shutdown(context.Background()))
	require.EqualValues(t, 1, exp.shutdowns.Load())
	require.EqualValues(t, 1, metricExp.shutdowns.Load())
}

func TestFromSDK_RequiresCompleteDependencies(t *testing.T) {
	// Arrange.
	sdk := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = sdk.Shutdown(context.Background()) })
	deps := SDKDependencies{
		TracerProvider: sdk,
		MeterProvider:  noop.NewMeterProvider(),
		Propagator:     propagation.TraceContext{},
	}
	var typedNil *sdktrace.TracerProvider
	// Act / Assert.
	for _, invalid := range []SDKDependencies{
		{}, {TracerProvider: typedNil, MeterProvider: deps.MeterProvider, Propagator: deps.Propagator},
		{TracerProvider: sdk, Propagator: deps.Propagator},
		{TracerProvider: sdk, MeterProvider: deps.MeterProvider},
	} {
		provider, err := FromSDK(invalid)
		require.ErrorIs(t, err, ErrInvalidSDKDependencies)
		require.Nil(t, provider)
	}
}

func TestExecutorInstruments_ProviderScopedConcurrent(t *testing.T) {
	// Arrange.
	p1, _ := newTestProvider(t)
	p2, _ := newTestProvider(t)
	var wg sync.WaitGroup
	observed := make(chan *executorInstruments, 32)
	// Act.
	for range cap(observed) {
		wg.Go(func() {
			inst, err := p1.executorInstruments()
			if err != nil {
				t.Error(err)
			}
			observed <- inst
		})
	}
	wg.Wait()
	close(observed)
	first := <-observed
	// Assert: all callers on one provider share one instance, never across providers.
	for inst := range observed {
		require.Same(t, first, inst)
	}
	other, err := p2.executorInstruments()
	require.NoError(t, err)
	require.NotSame(t, first, other)
	require.NoError(t, p1.Shutdown(context.Background()))
	require.Same(t, other, p2.executorMetrics)
}

func TestDefaultSampler_ParentAndRootMatrix(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, sampled := range []bool{false, true} {
			for _, ratio := range []float64{0, 1} {
				// Arrange.
				p, _ := newTestProvider(t, WithTraceRatio(ratio))
				flags := trace.TraceFlags(0)
				if sampled {
					flags = trace.FlagsSampled
				}
				parent := trace.NewSpanContext(
					trace.SpanContextConfig{
						TraceID:    trace.TraceID{1},
						SpanID:     trace.SpanID{1},
						TraceFlags: flags,
						Remote:     remote,
					},
				)
				// Act.
				_, child := p.SDK().
					TracerProvider.Tracer("test").
					Start(trace.ContextWithSpanContext(context.Background(), parent), "child")
				// Assert.
				require.Equal(t, sampled, child.SpanContext().IsSampled())
				child.End()
				_, root := p.SDK().TracerProvider.Tracer("test").Start(context.Background(), "root")
				require.Equal(t, ratio == 1, root.SpanContext().IsSampled())
				root.End()
				_, detached := p.SDK().
					TracerProvider.Tracer("test").
					Start(trace.ContextWithSpanContext(context.Background(), parent), "detached", trace.WithNewRoot(), trace.WithLinks(trace.Link{SpanContext: parent}))
				require.Equal(t, ratio == 1, detached.SpanContext().IsSampled())
				detached.End()
			}
		}
	}
	// Arrange: custom sampler must not be wrapped in ParentBased.
	p, _ := newTestProvider(t, WithTraceRatio(0), WithSampler(AlwaysSample()))
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}})
	// Act.
	_, child := p.SDK().
		TracerProvider.Tracer("test").
		Start(trace.ContextWithSpanContext(context.Background(), parent), "custom")
	// Assert.
	require.True(t, child.SpanContext().IsSampled())
	child.End()
}
