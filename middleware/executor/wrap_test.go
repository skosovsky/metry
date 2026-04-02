package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

func TestWrap_NilOrIncompleteProvider_Panics(t *testing.T) {
	next := func(context.Context, int) (int, error) { return 0, nil }

	require.Panics(t, func() {
		_ = Wrap(nil, "op", next)
	})
	require.Panics(t, func() {
		_ = Wrap(&metry.Provider{}, "op", next)
	})
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	require.Panics(t, func() {
		_ = Wrap(&metry.Provider{TracerProvider: tp}, "op", next)
	})
}

func TestWrap_NilNext_Panics(t *testing.T) {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("test-exec-nil-next"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	require.Panics(t, func() {
		_ = Wrap[int, int](provider, "op", nil)
	})
}

func TestWrap_Success_RecordsSpanAndMetrics(t *testing.T) {
	ctx := context.Background()
	traceMem := testutil.NewInMemoryTraceExporter()
	metricMem := testutil.NewInMemoryMetricExporter()

	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-exec-ok"),
		metry.WithExporter(traceMem.SpanExporter()),
		metry.WithMetricExporter(metricMem.Exporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	const op = "test.operation.ok"
	wrapped := Wrap(provider, op, func(_ context.Context, x int) (int, error) {
		return x * 2, nil
	})

	got, err := wrapped(ctx, 21)
	require.NoError(t, err)
	require.Equal(t, 42, got)

	tp, ok := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.True(t, ok)
	require.NoError(t, tp.ForceFlush(ctx))

	mpSDK, ok := provider.MeterProvider.(*sdkmetric.MeterProvider)
	require.True(t, ok)
	require.NoError(t, mpSDK.ForceFlush(ctx))

	spans := traceMem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, op, spans[0].Name)
	require.Equal(t, codes.Ok, spans[0].Status.Code)

	rm := metricMem.LastResourceMetrics()
	require.NotNil(t, rm)
	assert.InDelta(t, 1, int64CounterValue(t, *rm, op, "success"), 0)
	h := float64HistogramFor(t, *rm, durationMetricName, op, "success")
	require.GreaterOrEqual(t, h.Count, uint64(1))
	assert.Greater(t, h.Sum, 0.0)
}

func TestWrap_Error_RecordsSpanErrorAndMetrics(t *testing.T) {
	ctx := context.Background()
	traceMem := testutil.NewInMemoryTraceExporter()
	metricMem := testutil.NewInMemoryMetricExporter()

	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-exec-err"),
		metry.WithExporter(traceMem.SpanExporter()),
		metry.WithMetricExporter(metricMem.Exporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	wantErr := errors.New("boom")
	const op = "test.operation.err"
	wrapped := Wrap(provider, op, func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, wantErr
	})

	_, err = wrapped(ctx, struct{}{})
	require.ErrorIs(t, err, wantErr)

	tp := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.NoError(t, tp.ForceFlush(ctx))
	require.NoError(t, provider.MeterProvider.(*sdkmetric.MeterProvider).ForceFlush(ctx))

	spans := traceMem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status.Code)

	rm := metricMem.LastResourceMetrics()
	require.NotNil(t, rm)
	assert.InDelta(t, 1, int64CounterValue(t, *rm, op, "error"), 0)
	h := float64HistogramFor(t, *rm, durationMetricName, op, "error")
	require.GreaterOrEqual(t, h.Count, uint64(1))
}

func TestWrap_Panic_RecordsMetricsAndReraises(t *testing.T) {
	ctx := context.Background()
	traceMem := testutil.NewInMemoryTraceExporter()
	metricMem := testutil.NewInMemoryMetricExporter()

	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-exec-panic"),
		metry.WithExporter(traceMem.SpanExporter()),
		metry.WithMetricExporter(metricMem.Exporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	const op = "test.operation.panic"
	wrapped := Wrap(provider, op, func(context.Context, int) (int, error) {
		panic("abort")
	})

	require.Panics(t, func() { _, _ = wrapped(ctx, 0) })

	tp := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.NoError(t, tp.ForceFlush(ctx))
	require.NoError(t, provider.MeterProvider.(*sdkmetric.MeterProvider).ForceFlush(ctx))

	spans := traceMem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status.Code)

	rm := metricMem.LastResourceMetrics()
	require.NotNil(t, rm)
	assert.InDelta(t, 1, int64CounterValue(t, *rm, op, "panic"), 0)
	h := float64HistogramFor(t, *rm, durationMetricName, op, "panic")
	require.GreaterOrEqual(t, h.Count, uint64(1))
}

func TestWrap_StartLog_ContainsTraceID(t *testing.T) {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("test-exec-start-log"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	wrapped := Wrap(
		provider,
		"start.op",
		func(context.Context, int) (int, error) {
			return 42, nil
		},
		WithLogger(log),
		WithLogStart(true),
	)

	got, err := wrapped(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 42, got)

	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
	assert.Equal(t, "executor start", rec["msg"])
	assert.Contains(t, rec, "trace_id")
	assert.NotEmpty(t, rec["trace_id"])
}

func TestWrap_ErrorLog_ContainsTraceID(t *testing.T) {
	ctx := context.Background()
	traceMem := testutil.NewInMemoryTraceExporter()

	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-exec-log"),
		metry.WithExporter(traceMem.SpanExporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	wrapped := Wrap(
		provider,
		"log.op",
		func(context.Context, int) (int, error) {
			return 0, errors.New("fail")
		},
		WithLogger(log),
	)

	_, err = wrapped(ctx, 1)
	require.Error(t, err)

	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
	assert.Equal(t, "executor error", rec["msg"])
	assert.Contains(t, rec, "trace_id")
	assert.NotEmpty(t, rec["trace_id"])
}

// TestWrap_TwoWrapsSameProvider_ShareInstrumentCache ensures multiple Wrap calls on one MeterProvider
// still record distinct operations (shared cached instruments per provider+scope).
func TestWrap_TwoWrapsSameProvider_ShareInstrumentCache(t *testing.T) {
	ctx := context.Background()
	metricMem := testutil.NewInMemoryMetricExporter()

	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-exec-shared-instr"),
		metry.WithMetricExporter(metricMem.Exporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	wA := Wrap(provider, "op.a", func(context.Context, int) (int, error) { return 1, nil })
	wB := Wrap(provider, "op.b", func(context.Context, int) (int, error) { return 2, nil })

	_, err = wA(ctx, 0)
	require.NoError(t, err)
	_, err = wB(ctx, 0)
	require.NoError(t, err)

	mpSDK, ok := provider.MeterProvider.(*sdkmetric.MeterProvider)
	require.True(t, ok)
	require.NoError(t, mpSDK.ForceFlush(ctx))

	rm := metricMem.LastResourceMetrics()
	require.NotNil(t, rm)
	assert.InDelta(t, 1, int64CounterValue(t, *rm, "op.a", "success"), 0)
	assert.InDelta(t, 1, int64CounterValue(t, *rm, "op.b", "success"), 0)
}

func TestWrap_WithLogErrorFalse_SkipsErrorLog(t *testing.T) {
	ctx := context.Background()
	provider, err := metry.New(ctx, metry.WithServiceName("test-exec-no-log"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	wrapped := Wrap(
		provider,
		"silent.op",
		func(context.Context, int) (int, error) {
			return 0, errors.New("fail")
		},
		WithLogger(log),
		WithLogError(false),
	)

	_, err = wrapped(ctx, 1)
	require.Error(t, err)
	assert.Empty(t, buf.Bytes())
}

func int64CounterValue(t *testing.T, rm metricdata.ResourceMetrics, operation, status string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != callsMetricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				if attrString(dp.Attributes, "operation") == operation &&
					attrString(dp.Attributes, "status") == status {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("counter %q for operation=%q status=%q not found", callsMetricName, operation, status)
	return 0
}

func float64HistogramFor(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name, operation, status string,
) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range hist.DataPoints {
				if attrString(dp.Attributes, "operation") == operation &&
					attrString(dp.Attributes, "status") == status {
					return dp
				}
			}
		}
	}
	t.Fatalf("histogram %q for operation=%q status=%q not found", name, operation, status)
	return metricdata.HistogramDataPoint[float64]{}
}

func attrString(attrs attribute.Set, key string) string {
	v, ok := attrs.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return v.AsString()
}
