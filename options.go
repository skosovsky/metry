// Package metry provides a zero-boilerplate OpenTelemetry and LLMOps hub for Go AI applications.
package metry

import (
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Option configures Init. Use WithServiceName, WithTraceRatio, etc.
type Option func(*config)

// config holds Init options with defaults applied.
type config struct {
	ServiceName           string
	ServiceVersion        string
	Environment           string
	TraceRatio            float64
	Exporter              sdktrace.SpanExporter
	MetricExporter        sdkmetric.Exporter
	RecordPayloads        bool
	maxGenAIContextLength int
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

// WithExporter sets the span exporter. If not set, a no-op exporter is used.
func WithExporter(exp sdktrace.SpanExporter) Option {
	return func(c *config) { c.Exporter = exp }
}

// WithMetricExporter sets the metric exporter. If not set, metrics are not exported.
func WithMetricExporter(exp sdkmetric.Exporter) Option {
	return func(c *config) { c.MetricExporter = exp }
}

// WithRecordPayloads enables or disables prompt/completion/system payload recording.
func WithRecordPayloads(enabled bool) Option {
	return func(c *config) { c.RecordPayloads = enabled }
}

// WithMaxGenAIContextLength sets the max byte length for prompt/completion/tool strings before truncation (default 16384).
// Use for local dev or closed loops where full context is needed; 0 means keep default.
func WithMaxGenAIContextLength(bytes int) Option {
	return func(c *config) { c.maxGenAIContextLength = bytes }
}
