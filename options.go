// Package metry provides a zero-boilerplate OpenTelemetry and LLMOps hub for Go AI applications.
package metry

import (
	"context"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TraceExporter is an opaque type that produces an OTel trace exporter.
// Create it via OTLPGRPC, OTLPHTTP, Console, Noop, or testutil.
type TraceExporter struct {
	create traceExporterFactory
}

type traceExporterFactory func(ctx context.Context, res *resource.Resource) (sdktrace.SpanExporter, error)

// MetricExporter is an opaque type that produces an OTel metric exporter.
// Create it via OTLPGRPC, OTLPHTTP, Console, Noop, or testutil.
type MetricExporter struct {
	create metricExporterFactory
}

type metricExporterFactory func(ctx context.Context, res *resource.Resource) (metric.Exporter, error)

// Options configures the full OTel stack for a service.
type Options struct {
	// ServiceName is the name of the service (required).
	ServiceName string
	// ServiceVersion is the release version (optional).
	ServiceVersion string
	// Environment is the deployment environment, e.g. "production", "staging", "local".
	Environment string

	// TraceExporter exports trace spans. If nil, a no-op exporter is used.
	TraceExporter *TraceExporter
	// MetricExporter exports metrics. If nil, a no-op exporter is used.
	MetricExporter *MetricExporter

	// TraceRatio is the fraction of traces to sample (1.0 = 100%, 0.1 = 10%).
	// If nil, 1.0 is used. Use Float64(0) to disable tracing (0% sampling).
	TraceRatio *float64
}

// Float64 returns a pointer to v. Use for Options.TraceRatio to distinguish
// nil (default 1.0) from explicit 0.0 (disable sampling).
//
//go:fix inline
func Float64(v float64) *float64 {
	return new(v)
}

// NewTraceExporterFromSpanExporter wraps an existing sdktrace.SpanExporter as a TraceExporter.
// Used by testutil and other packages that need to plug in a custom exporter instance.
func NewTraceExporterFromSpanExporter(exp sdktrace.SpanExporter) *TraceExporter {
	return &TraceExporter{
		create: func(context.Context, *resource.Resource) (sdktrace.SpanExporter, error) {
			return exp, nil
		},
	}
}

// NewMetricExporterFromExporter wraps an existing metric.Exporter as a MetricExporter.
// Used by testutil and other packages that need to plug in a custom exporter instance.
func NewMetricExporterFromExporter(exp metric.Exporter) *MetricExporter {
	return &MetricExporter{
		create: func(context.Context, *resource.Resource) (metric.Exporter, error) {
			return exp, nil
		},
	}
}
