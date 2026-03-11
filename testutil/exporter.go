// Package testutil provides in-memory exporters and helpers for testing code that uses metry.
package testutil

import (
	"context"
	"sync"
	"testing"

	"github.com/skosovsky/metry"
	"go.opentelemetry.io/otel"
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

// TraceExporter returns a metry.TraceExporter that sends spans to this in-memory store.
// Use it in metry.Options when calling metry.Init.
func (e *InMemoryTraceExporter) TraceExporter() *metry.TraceExporter {
	return metry.NewTraceExporterFromSpanExporter(e.ex)
}

// InMemoryMetricExporter stores the count of Export calls for test assertions.
// It implements sdkmetric.Exporter so it can be used with metry.Init.
type InMemoryMetricExporter struct {
	mu    sync.Mutex
	count int
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

// Export implements sdkmetric.Exporter and increments the export count.
func (e *InMemoryMetricExporter) Export(_ context.Context, _ *metricdata.ResourceMetrics) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count++
	return nil
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

// Reset clears the export count.
func (e *InMemoryMetricExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count = 0
}

// Len returns the number of Export calls received.
func (e *InMemoryMetricExporter) Len() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

// MetricExporter returns a metry.MetricExporter that sends metrics to this in-memory store.
func (e *InMemoryMetricExporter) MetricExporter() *metry.MetricExporter {
	return metry.NewMetricExporterFromExporter(e)
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
