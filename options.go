// Package metry provides a zero-boilerplate OpenTelemetry and LLMOps hub for Go AI applications.
package metry

import (
	"context"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Option configures Init. Use WithServiceName, WithTraceRatio, etc.
type Option func(*config)

// config holds Init options with defaults applied.
type config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	TraceRatio     float64
	TraceExporter  *TraceExporter
	MetricExporter *MetricExporter
}

// newConfig returns config with defaults (e.g. TraceRatio = 1.0).
func newConfig() *config {
	return &config{
		TraceRatio: 1.0,
	}
}

// WithServiceName sets the service name (required).
func WithServiceName(name string) Option {
	return func(c *config) { c.ServiceName = name }
}

// WithServiceVersion sets the service version (optional).
func WithServiceVersion(version string) Option {
	return func(c *config) { c.ServiceVersion = version }
}

// WithEnvironment sets the deployment environment (e.g. "production", "staging").
func WithEnvironment(env string) Option {
	return func(c *config) { c.Environment = env }
}

// WithTraceRatio sets the fraction of traces to sample (1.0 = 100%, 0.0 = disable).
func WithTraceRatio(ratio float64) Option {
	return func(c *config) { c.TraceRatio = ratio }
}

// WithTraceExporter sets the trace exporter. If not set, a no-op exporter is used.
func WithTraceExporter(te *TraceExporter) Option {
	return func(c *config) { c.TraceExporter = te }
}

// WithMetricExporter sets the metric exporter. If not set, metrics are not exported.
func WithMetricExporter(me *MetricExporter) Option {
	return func(c *config) { c.MetricExporter = me }
}

// WithOTLPGRPC sets trace and metric exporters for OTLP over gRPC (e.g. "localhost:4317").
func WithOTLPGRPC(endpoint string, insecure bool) Option {
	te, me := OTLPGRPC(endpoint, insecure)
	return func(c *config) {
		c.TraceExporter = te
		c.MetricExporter = me
	}
}

// WithOTLPHTTP sets trace and metric exporters for OTLP over HTTP (e.g. "localhost:4318").
func WithOTLPHTTP(endpoint string, headers map[string]string) Option {
	te, me := OTLPHTTP(endpoint, headers)
	return func(c *config) {
		c.TraceExporter = te
		c.MetricExporter = me
	}
}

// WithConsole sets trace and metric exporters that write to stdout (for local dev).
func WithConsole() Option {
	te, me := Console()
	return func(c *config) {
		c.TraceExporter = te
		c.MetricExporter = me
	}
}

// WithNoop sets no-op trace and metric exporters (disable telemetry or tests).
func WithNoop() Option {
	te, me := Noop()
	return func(c *config) {
		c.TraceExporter = te
		c.MetricExporter = me
	}
}

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
