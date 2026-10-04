// Package metry provides a zero-boilerplate OpenTelemetry and LLMOps hub for Go AI applications.
package metry

import (
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Option configures New. Use WithServiceName, WithTraceRatio, etc.
type Option func(*config)

// config holds New options with defaults applied.
type config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	TraceRatio     float64
	Sampler        sdktrace.Sampler
	Exporter       sdktrace.SpanExporter
	MetricExporter sdkmetric.Exporter
	invalid        bool
}

// newConfig returns config with defaults (e.g. TraceRatio = 1.0).
//
//nolint:exhaustruct_v5 // optional SDK interfaces default to nil
func newConfig() *config {
	return &config{
		ServiceName:    "",
		ServiceVersion: "",
		Environment:    "",
		TraceRatio:     1.0,
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

// WithTraceRatio sets the fraction of new root traces to sample; children inherit their parent.
func WithTraceRatio(ratio float64) Option {
	return func(c *config) { c.TraceRatio = ratio }
}

// WithSampler sets a custom head-based sampler for tracing.
// When provided, this sampler takes precedence over WithTraceRatio and is not wrapped.
func WithSampler(sampler sdktrace.Sampler) Option {
	return func(c *config) { c.invalid = c.invalid || nilDependency(sampler); c.Sampler = sampler }
}

// WithExporter sets the span exporter. If not set, a no-op exporter is used.
func WithExporter(exp sdktrace.SpanExporter) Option {
	return func(c *config) { c.invalid = c.invalid || nilDependency(exp); c.Exporter = exp }
}

// WithMetricExporter sets the metric exporter. If not set, metrics are not exported.
func WithMetricExporter(exp sdkmetric.Exporter) Option {
	return func(c *config) { c.invalid = c.invalid || nilDependency(exp); c.MetricExporter = exp }
}
