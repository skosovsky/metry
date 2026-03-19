// Package testutil provides in-memory exporters and helpers for testing code that uses metry.
package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// InMemoryTraceExporter stores spans in memory for test assertions.
type InMemoryTraceExporter struct {
	ex *tracetest.InMemoryExporter
}

// NewInMemoryTraceExporter returns a new in-memory trace exporter.
func NewInMemoryTraceExporter() *InMemoryTraceExporter {
	return &InMemoryTraceExporter{ex: tracetest.NewInMemoryExporter()}
}

// GetSpans returns a copy of the stored span stubs.
func (e *InMemoryTraceExporter) GetSpans() tracetest.SpanStubs {
	return e.ex.GetSpans()
}

// Reset clears all stored spans.
func (e *InMemoryTraceExporter) Reset() {
	e.ex.Reset()
}

// Len returns the number of stored spans.
func (e *InMemoryTraceExporter) Len() int {
	return len(e.ex.GetSpans())
}

// SpanExporter returns the underlying sdktrace.SpanExporter.
func (e *InMemoryTraceExporter) SpanExporter() sdktrace.SpanExporter {
	return e.ex
}

// InMemoryMetricExporter stores the count of Export calls and the last ResourceMetrics
// for test assertions (e.g. lifecycle tests that need to assert on datapoints).
// Contract: unsupported aggregation types (e.g. Gauge) cause panic in Export (fail-fast by design).
type InMemoryMetricExporter struct {
	mu     sync.Mutex
	count  int
	lastRM *metricdata.ResourceMetrics
}

// NewInMemoryMetricExporter returns a new in-memory metric exporter.
func NewInMemoryMetricExporter() *InMemoryMetricExporter {
	return &InMemoryMetricExporter{}
}

// Temporality implements sdkmetric.Exporter.
func (e *InMemoryMetricExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(k)
}

// Aggregation implements sdkmetric.Exporter.
func (e *InMemoryMetricExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}

// Export implements sdkmetric.Exporter; it increments the export count and stores a deep copy of the payload.
// A snapshot is required because the SDK may reuse the same *ResourceMetrics buffer across export cycles.
// Unsupported aggregation types (e.g. Gauge) cause panic before copy (fail-fast contract).
func (e *InMemoryMetricExporter) Export(_ context.Context, rm *metricdata.ResourceMetrics) error {
	checkSupportedAggregations(rm)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count++
	e.lastRM = deepCopyResourceMetrics(rm)
	return nil
}

// checkSupportedAggregations panics if rm contains any aggregation type not supported by deepCopyMetrics.
func checkSupportedAggregations(rm *metricdata.ResourceMetrics) {
	if rm == nil {
		return
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Data.(type) {
			case metricdata.Sum[int64], metricdata.Sum[float64], metricdata.Histogram[float64]:
				continue
			default:
				panic(fmt.Sprintf("testutil: Export does not support aggregation type %T (e.g. Gauge)", m.Data))
			}
		}
	}
}

// LastResourceMetrics returns a deep copy of the last ResourceMetrics passed to Export, or nil.
// Safe to call after shutdown; returned value is independent of SDK buffers.
func (e *InMemoryMetricExporter) LastResourceMetrics() *metricdata.ResourceMetrics {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lastRM == nil {
		return nil
	}
	return deepCopyResourceMetrics(e.lastRM)
}

// ForceFlush implements sdkmetric.Exporter.
func (e *InMemoryMetricExporter) ForceFlush(context.Context) error { return nil }

// Shutdown implements sdkmetric.Exporter.
func (e *InMemoryMetricExporter) Shutdown(context.Context) error { return nil }

// GetMetrics returns the number of Export calls received.
func (e *InMemoryMetricExporter) GetMetrics() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

// Reset clears the export count and the last snapshot so tests do not see stale data.
func (e *InMemoryMetricExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count = 0
	e.lastRM = nil
}

// Len returns the number of Export calls received.
func (e *InMemoryMetricExporter) Len() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

// Exporter returns the underlying sdkmetric.Exporter.
func (e *InMemoryMetricExporter) Exporter() sdkmetric.Exporter {
	return e
}

// deepCopyResourceMetrics returns an independent copy of rm so SDK buffer reuse cannot affect tests.
func deepCopyResourceMetrics(rm *metricdata.ResourceMetrics) *metricdata.ResourceMetrics {
	if rm == nil {
		return nil
	}
	out := &metricdata.ResourceMetrics{
		Resource:     rm.Resource,
		ScopeMetrics: make([]metricdata.ScopeMetrics, len(rm.ScopeMetrics)),
	}
	for i := range rm.ScopeMetrics {
		out.ScopeMetrics[i] = deepCopyScopeMetrics(rm.ScopeMetrics[i])
	}
	return out
}

func deepCopyScopeMetrics(sm metricdata.ScopeMetrics) metricdata.ScopeMetrics {
	out := metricdata.ScopeMetrics{
		Scope:   sm.Scope,
		Metrics: make([]metricdata.Metrics, len(sm.Metrics)),
	}
	for i := range sm.Metrics {
		out.Metrics[i] = deepCopyMetrics(sm.Metrics[i])
	}
	return out
}

// deepCopyMetrics returns an independent copy of the metric. Unsupported aggregation types
// (e.g. Gauge) cause panic by design (fail-fast contract for testutil).
func deepCopyMetrics(m metricdata.Metrics) metricdata.Metrics {
	out := metricdata.Metrics{
		Name:        m.Name,
		Description: m.Description,
		Unit:        m.Unit,
	}
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		out.Data = deepCopySumInt64(d)
	case metricdata.Sum[float64]:
		out.Data = deepCopySumFloat64(d)
	case metricdata.Histogram[float64]:
		out.Data = deepCopyHistogramFloat64(d)
	default:
		panic(fmt.Sprintf("testutil: deepCopyMetrics does not support aggregation type %T", d))
	}
	return out
}

func deepCopyExemplar[N int64 | float64](ex metricdata.Exemplar[N]) metricdata.Exemplar[N] {
	exCopy := metricdata.Exemplar[N]{
		Time:  ex.Time,
		Value: ex.Value,
	}
	if len(ex.FilteredAttributes) > 0 {
		exCopy.FilteredAttributes = make([]attribute.KeyValue, len(ex.FilteredAttributes))
		copy(exCopy.FilteredAttributes, ex.FilteredAttributes)
	}
	if len(ex.SpanID) > 0 {
		exCopy.SpanID = append([]byte(nil), ex.SpanID...)
	}
	if len(ex.TraceID) > 0 {
		exCopy.TraceID = append([]byte(nil), ex.TraceID...)
	}
	return exCopy
}

func deepCopyDataPointInt64(dp metricdata.DataPoint[int64]) metricdata.DataPoint[int64] {
	out := metricdata.DataPoint[int64]{
		Attributes: dp.Attributes,
		StartTime:  dp.StartTime,
		Time:       dp.Time,
		Value:      dp.Value,
	}
	if len(dp.Exemplars) > 0 {
		out.Exemplars = make([]metricdata.Exemplar[int64], len(dp.Exemplars))
		for i := range dp.Exemplars {
			out.Exemplars[i] = deepCopyExemplar(dp.Exemplars[i])
		}
	}
	return out
}

func deepCopyDataPointFloat64(dp metricdata.DataPoint[float64]) metricdata.DataPoint[float64] {
	out := metricdata.DataPoint[float64]{
		Attributes: dp.Attributes,
		StartTime:  dp.StartTime,
		Time:       dp.Time,
		Value:      dp.Value,
	}
	if len(dp.Exemplars) > 0 {
		out.Exemplars = make([]metricdata.Exemplar[float64], len(dp.Exemplars))
		for i := range dp.Exemplars {
			out.Exemplars[i] = deepCopyExemplar(dp.Exemplars[i])
		}
	}
	return out
}

func deepCopySumInt64(s metricdata.Sum[int64]) metricdata.Sum[int64] {
	out := metricdata.Sum[int64]{
		DataPoints:  make([]metricdata.DataPoint[int64], len(s.DataPoints)),
		Temporality: s.Temporality,
		IsMonotonic: s.IsMonotonic,
	}
	for i := range s.DataPoints {
		out.DataPoints[i] = deepCopyDataPointInt64(s.DataPoints[i])
	}
	return out
}

func deepCopySumFloat64(s metricdata.Sum[float64]) metricdata.Sum[float64] {
	out := metricdata.Sum[float64]{
		DataPoints:  make([]metricdata.DataPoint[float64], len(s.DataPoints)),
		Temporality: s.Temporality,
		IsMonotonic: s.IsMonotonic,
	}
	for i := range s.DataPoints {
		out.DataPoints[i] = deepCopyDataPointFloat64(s.DataPoints[i])
	}
	return out
}

func deepCopyHistogramDataPointFloat64(dp metricdata.HistogramDataPoint[float64]) metricdata.HistogramDataPoint[float64] {
	out := metricdata.HistogramDataPoint[float64]{
		Attributes:   dp.Attributes,
		StartTime:    dp.StartTime,
		Time:         dp.Time,
		Count:        dp.Count,
		Bounds:       append([]float64(nil), dp.Bounds...),
		BucketCounts: append([]uint64(nil), dp.BucketCounts...),
		Min:          dp.Min,
		Max:          dp.Max,
		Sum:          dp.Sum,
	}
	if len(dp.Exemplars) > 0 {
		out.Exemplars = make([]metricdata.Exemplar[float64], len(dp.Exemplars))
		for i := range dp.Exemplars {
			out.Exemplars[i] = deepCopyExemplar(dp.Exemplars[i])
		}
	}
	return out
}

func deepCopyHistogramFloat64(h metricdata.Histogram[float64]) metricdata.Histogram[float64] {
	out := metricdata.Histogram[float64]{
		DataPoints:  make([]metricdata.HistogramDataPoint[float64], len(h.DataPoints)),
		Temporality: h.Temporality,
	}
	for i := range h.DataPoints {
		out.DataPoints[i] = deepCopyHistogramDataPointFloat64(h.DataPoints[i])
	}
	return out
}

// SetupTestTracing configures the global tracer with an in-memory exporter using
// a synchronous SimpleSpanProcessor so spans are available immediately for assertions.
// Registers cleanup on t. Returns the InMemoryTraceExporter for assertions.
func SetupTestTracing(t *testing.T) *InMemoryTraceExporter {
	t.Helper()
	mem := NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(mem.ex)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return mem
}

// SetupTestMetrics configures the global meter provider with a ManualReader so tests can
// collect metrics synchronously via reader.Collect. Registers cleanup on t.
// Returns the reader (for Collect and assertions) and the meter (for creating instruments).
func SetupTestMetrics(t *testing.T) (*sdkmetric.ManualReader, metric.Meter) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	return reader, mp.Meter("test")
}
