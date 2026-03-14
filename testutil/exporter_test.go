package testutil

import (
	"context"
	"testing"

	"github.com/skosovsky/metry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNewInMemoryTraceExporter_InitialState(t *testing.T) {
	mem := NewInMemoryTraceExporter()
	require.NotNil(t, mem)
	assert.Equal(t, 0, mem.Len())
	spans := mem.GetSpans()
	assert.Empty(t, spans)
	mem.Reset()
	assert.Equal(t, 0, mem.Len())
}

func TestInMemoryTraceExporter_TraceExporter(t *testing.T) {
	mem := NewInMemoryTraceExporter()
	te := mem.TraceExporter()
	require.NotNil(t, te)
	// Use SimpleSpanProcessor so spans are exported synchronously for deterministic test.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(mem.ex)),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := metry.GlobalTracer()
	_, span := tracer.Start(context.Background(), "op")
	span.End()
	assert.Equal(t, 1, mem.Len())
}

func TestSetupTestTracing_ReturnsExporter(t *testing.T) {
	mem := SetupTestTracing(t)
	require.NotNil(t, mem)
	tracer := metry.GlobalTracer()
	require.NotNil(t, tracer)
	_, span := tracer.Start(context.Background(), "test")
	span.End()
	assert.Equal(t, 1, mem.Len())
}

func TestSetupTestMetrics_ReturnsReaderAndMeter(t *testing.T) {
	reader, meter := SetupTestMetrics(t)
	require.NotNil(t, reader)
	require.NotNil(t, meter)
}

func TestInMemoryMetricExporter_GetMetrics_Reset_Len(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	require.NotNil(t, mem)
	assert.Equal(t, 0, mem.Len())

	ctx := context.Background()
	err := mem.Export(ctx, &metricdata.ResourceMetrics{})
	require.NoError(t, err)
	assert.Equal(t, 1, mem.Len())
	assert.Equal(t, 1, mem.GetMetrics())

	mem.Reset()
	assert.Equal(t, 0, mem.Len())
}

// makeRMWithSumValue builds a minimal ResourceMetrics with one Sum[int64] metric having one data point.
func makeRMWithSumValue(name string, value int64) *metricdata.ResourceMetrics {
	return &metricdata.ResourceMetrics{
		ScopeMetrics: []metricdata.ScopeMetrics{{
			Metrics: []metricdata.Metrics{{
				Name: name,
				Data: metricdata.Sum[int64]{
					DataPoints: []metricdata.DataPoint[int64]{{Value: value}},
				},
			}},
		}},
	}
}

// getFirstSumInt64Value returns the first Sum[int64] data point value in rm, or 0 and false if not found.
func getFirstSumInt64Value(rm metricdata.ResourceMetrics) (int64, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if s, ok := m.Data.(metricdata.Sum[int64]); ok && len(s.DataPoints) > 0 {
				return s.DataPoints[0].Value, true
			}
		}
	}
	return 0, false
}

func TestInMemoryMetricExporter_LastResourceMetrics_ReturnsDataAfterExport(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	rm := makeRMWithSumValue("test.counter", 42)
	err := mem.Export(ctx, rm)
	require.NoError(t, err)

	last := mem.LastResourceMetrics()
	require.NotNil(t, last)
	val, ok := getFirstSumInt64Value(*last)
	require.True(t, ok)
	assert.Equal(t, int64(42), val)
}

func TestInMemoryMetricExporter_Reset_ClearsCountAndLastRM(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	err := mem.Export(ctx, makeRMWithSumValue("x", 1))
	require.NoError(t, err)
	require.NotNil(t, mem.LastResourceMetrics())

	mem.Reset()
	assert.Equal(t, 0, mem.Len())
	assert.Nil(t, mem.LastResourceMetrics())
}

func TestInMemoryMetricExporter_MultipleExports_LastWins(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	err := mem.Export(ctx, makeRMWithSumValue("a", 1))
	require.NoError(t, err)
	err = mem.Export(ctx, makeRMWithSumValue("b", 2))
	require.NoError(t, err)

	last := mem.LastResourceMetrics()
	require.NotNil(t, last)
	val, ok := getFirstSumInt64Value(*last)
	require.True(t, ok)
	assert.Equal(t, int64(2), val)
	assert.Equal(t, 2, mem.Len())
}

func TestInMemoryMetricExporter_SnapshotIndependentOfCallerMutation(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	rm := makeRMWithSumValue("m", 10)
	err := mem.Export(ctx, rm)
	require.NoError(t, err)

	last := mem.LastResourceMetrics()
	require.NotNil(t, last)
	val, ok := getFirstSumInt64Value(*last)
	require.True(t, ok)
	assert.Equal(t, int64(10), val)

	// Mutate the original; snapshot must be unchanged
	rm.ScopeMetrics[0].Metrics[0].Data = metricdata.Sum[int64]{
		DataPoints: []metricdata.DataPoint[int64]{{Value: 99}},
	}
	last2 := mem.LastResourceMetrics()
	require.NotNil(t, last2)
	val2, ok := getFirstSumInt64Value(*last2)
	require.True(t, ok)
	assert.Equal(t, int64(10), val2, "snapshot must be independent of original after Export")
}

// TestInMemoryMetricExporter_MutatingSnapshotDoesNotAffectStoredSnapshot verifies that
// mutating a returned snapshot does not change the stored snapshot (each LastResourceMetrics returns a new copy).
func TestInMemoryMetricExporter_MutatingSnapshotDoesNotAffectStoredSnapshot(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	rm := makeRMWithSumValue("m", 10)
	err := mem.Export(ctx, rm)
	require.NoError(t, err)

	last1 := mem.LastResourceMetrics()
	require.NotNil(t, last1)
	val1, ok := getFirstSumInt64Value(*last1)
	require.True(t, ok)
	require.Equal(t, int64(10), val1)

	// Mutate the returned snapshot; stored snapshot must be unchanged
	for i := range last1.ScopeMetrics {
		for j := range last1.ScopeMetrics[i].Metrics {
			if s, ok := last1.ScopeMetrics[i].Metrics[j].Data.(metricdata.Sum[int64]); ok && len(s.DataPoints) > 0 {
				s.DataPoints[0].Value = 99
				last1.ScopeMetrics[i].Metrics[j].Data = s
				break
			}
		}
	}

	last2 := mem.LastResourceMetrics()
	require.NotNil(t, last2)
	val2, ok := getFirstSumInt64Value(*last2)
	require.True(t, ok)
	assert.Equal(t, int64(10), val2, "mutating a returned snapshot must not affect the stored snapshot")
}

// makeRMWithSumAndExemplar builds ResourceMetrics with one Sum[int64] datapoint that has one Exemplar.
func makeRMWithSumAndExemplar(value int64, exemplarValue int64) *metricdata.ResourceMetrics {
	return &metricdata.ResourceMetrics{
		ScopeMetrics: []metricdata.ScopeMetrics{{
			Metrics: []metricdata.Metrics{{
				Name: "counter.with.exemplar",
				Data: metricdata.Sum[int64]{
					DataPoints: []metricdata.DataPoint[int64]{{
						Value: value,
						Exemplars: []metricdata.Exemplar[int64]{{
							Value:              exemplarValue,
							FilteredAttributes: []attribute.KeyValue{attribute.Int("ex", 1)},
							SpanID:             []byte{1, 2, 3},
							TraceID:            []byte{4, 5, 6},
						}},
					}},
				},
			}},
		}},
	}
}

func getFirstExemplarValueInt64(rm metricdata.ResourceMetrics) (int64, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if s, ok := m.Data.(metricdata.Sum[int64]); ok && len(s.DataPoints) > 0 && len(s.DataPoints[0].Exemplars) > 0 {
				return s.DataPoints[0].Exemplars[0].Value, true
			}
		}
	}
	return 0, false
}

func TestInMemoryMetricExporter_SnapshotIndependentOfOriginal_WithExemplars(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	rm := makeRMWithSumAndExemplar(10, 5)
	err := mem.Export(ctx, rm)
	require.NoError(t, err)

	last := mem.LastResourceMetrics()
	require.NotNil(t, last)
	exVal, ok := getFirstExemplarValueInt64(*last)
	require.True(t, ok)
	assert.Equal(t, int64(5), exVal)

	// Mutate original's exemplar value; snapshot must be unchanged
	rm.ScopeMetrics[0].Metrics[0].Data = metricdata.Sum[int64]{
		DataPoints: []metricdata.DataPoint[int64]{{
			Value:     10,
			Exemplars: []metricdata.Exemplar[int64]{{Value: 99}},
		}},
	}
	last2 := mem.LastResourceMetrics()
	require.NotNil(t, last2)
	exVal2, ok := getFirstExemplarValueInt64(*last2)
	require.True(t, ok)
	assert.Equal(t, int64(5), exVal2, "snapshot exemplar must be independent of original")
}

// makeRMWithHistogramAndExemplar builds ResourceMetrics with one Histogram[float64] datapoint that has one Exemplar.
func makeRMWithHistogramAndExemplar(histSum float64, exemplarValue float64) *metricdata.ResourceMetrics {
	return &metricdata.ResourceMetrics{
		ScopeMetrics: []metricdata.ScopeMetrics{{
			Metrics: []metricdata.Metrics{{
				Name: "histogram.with.exemplar",
				Data: metricdata.Histogram[float64]{
					DataPoints: []metricdata.HistogramDataPoint[float64]{{
						Count:  1,
						Sum:    histSum,
						Bounds: []float64{1, 10},
						BucketCounts: []uint64{0, 1},
						Exemplars: []metricdata.Exemplar[float64]{{
							Value:              exemplarValue,
							FilteredAttributes: []attribute.KeyValue{attribute.Float64("ex", 1)},
							SpanID:             []byte{7, 8, 9},
							TraceID:            []byte{10, 11, 12},
						}},
					}},
				},
			}},
		}},
	}
}

func getFirstHistogramExemplarValue(rm metricdata.ResourceMetrics) (float64, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if h, ok := m.Data.(metricdata.Histogram[float64]); ok && len(h.DataPoints) > 0 && len(h.DataPoints[0].Exemplars) > 0 {
				return h.DataPoints[0].Exemplars[0].Value, true
			}
		}
	}
	return 0, false
}

// TestInMemoryMetricExporter_HistogramExemplarSnapshotIndependentOfOriginal verifies that
// after Export of ResourceMetrics containing Histogram with Exemplars, mutating the original
// does not change LastResourceMetrics() (deep copy includes histogram exemplars).
func TestInMemoryMetricExporter_HistogramExemplarSnapshotIndependentOfOriginal(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	rm := makeRMWithHistogramAndExemplar(2.5, 1.5)
	err := mem.Export(ctx, rm)
	require.NoError(t, err)

	last := mem.LastResourceMetrics()
	require.NotNil(t, last)
	exVal, ok := getFirstHistogramExemplarValue(*last)
	require.True(t, ok)
	assert.InDelta(t, 1.5, exVal, 1e-9)

	// Mutate original's histogram exemplar; snapshot must be unchanged
	rm.ScopeMetrics[0].Metrics[0].Data = metricdata.Histogram[float64]{
		DataPoints: []metricdata.HistogramDataPoint[float64]{{
			Count: 1, Sum: 2.5, Bounds: []float64{1, 10}, BucketCounts: []uint64{0, 1},
			Exemplars: []metricdata.Exemplar[float64]{{Value: 999}},
		}},
	}
	last2 := mem.LastResourceMetrics()
	require.NotNil(t, last2)
	exVal2, ok := getFirstHistogramExemplarValue(*last2)
	require.True(t, ok)
	assert.InDelta(t, 1.5, exVal2, 1e-9, "snapshot histogram exemplar must be independent of original")
}

// TestInMemoryMetricExporter_HistogramExemplarMutatingSnapshotDoesNotAffectStored verifies that
// mutating a returned snapshot (with Histogram exemplars) does not change the stored snapshot.
func TestInMemoryMetricExporter_HistogramExemplarMutatingSnapshotDoesNotAffectStored(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	rm := makeRMWithHistogramAndExemplar(3.0, 2.0)
	err := mem.Export(ctx, rm)
	require.NoError(t, err)

	last1 := mem.LastResourceMetrics()
	require.NotNil(t, last1)
	exVal1, ok := getFirstHistogramExemplarValue(*last1)
	require.True(t, ok)
	assert.InDelta(t, 2.0, exVal1, 1e-9)

	// Mutate the returned snapshot's histogram exemplar
	for i := range last1.ScopeMetrics {
		for j := range last1.ScopeMetrics[i].Metrics {
			if h, ok := last1.ScopeMetrics[i].Metrics[j].Data.(metricdata.Histogram[float64]); ok && len(h.DataPoints) > 0 && len(h.DataPoints[0].Exemplars) > 0 {
				h.DataPoints[0].Exemplars[0].Value = 999
				last1.ScopeMetrics[i].Metrics[j].Data = h
				break
			}
		}
	}

	last2 := mem.LastResourceMetrics()
	require.NotNil(t, last2)
	exVal2, ok := getFirstHistogramExemplarValue(*last2)
	require.True(t, ok)
	assert.InDelta(t, 2.0, exVal2, 1e-9, "mutating returned snapshot must not affect stored snapshot")
}

func TestInMemoryMetricExporter_ExportPanicsForUnsupportedAggregationType(t *testing.T) {
	mem := NewInMemoryMetricExporter()
	ctx := context.Background()
	rm := &metricdata.ResourceMetrics{
		ScopeMetrics: []metricdata.ScopeMetrics{{
			Metrics: []metricdata.Metrics{{
				Name: "gauge.unsupported",
				Data: metricdata.Gauge[int64]{DataPoints: []metricdata.DataPoint[int64]{{Value: 1}}},
			}},
		}},
	}
	var panicVal interface{}
	require.Panics(t, func() {
		defer func() { panicVal = recover() }()
		_ = mem.Export(ctx, rm)
	}, "Export must panic for unsupported aggregation type")
	msg, ok := panicVal.(string)
	require.True(t, ok, "panic value must be string")
	assert.Contains(t, msg, "does not support", "panic message must describe unsupported type")
	assert.Contains(t, msg, "Gauge", "panic message must include aggregation type name for diagnostics")
}
