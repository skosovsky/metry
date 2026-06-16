package metrytest

import (
	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/metrytestwire"
	"github.com/skosovsky/metry/testutil"
)

// MetrySpanExporter wraps a testutil in-memory trace exporter for metry.WithExporter.
func MetrySpanExporter(e *testutil.InMemoryTraceExporter) metry.SpanExporter {
	return mustAs[metry.SpanExporter](metrytestwire.SpanExporter(e.SDKSpanExporter()), "SpanExporter")
}

// MetryMetricExporter wraps a testutil in-memory metric exporter for metry.WithMetricExporter.
func MetryMetricExporter(e *testutil.InMemoryMetricExporter) metry.MetricExporter {
	return mustAs[metry.MetricExporter](metrytestwire.MetricExporter(e.SDKExporter()), "MetricExporter")
}
