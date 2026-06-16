// Package metry provides a zero-boilerplate OpenTelemetry and LLMOps hub for Go AI applications.
package metry

// Option configures New. Use WithServiceName, WithTraceRatio, etc.
type Option func(*config)

// config holds New options with defaults applied.
type config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	TraceRatio     float64
	Sampler        TraceSampler
	Exporter       SpanExporter
	MetricExporter MetricExporter
}

// newConfig returns config with defaults (e.g. TraceRatio = 1.0).
//
//nolint:exhaustruct // opaque sampler/exporter wrappers default to zero values
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

// WithTraceRatio sets the fraction of traces to sample (1.0 = 100%, 0.0 = disable).
func WithTraceRatio(ratio float64) Option {
	return func(c *config) { c.TraceRatio = ratio }
}

// WithSampler sets a custom head-based sampler for tracing.
// When provided, this sampler takes precedence over WithTraceRatio.
func WithSampler(sampler TraceSampler) Option {
	return func(c *config) { c.Sampler = sampler }
}

// WithExporter sets the span exporter. If not set, a no-op exporter is used.
func WithExporter(exp SpanExporter) Option {
	return func(c *config) { c.Exporter = exp }
}

// WithMetricExporter sets the metric exporter. If not set, metrics are not exported.
func WithMetricExporter(exp MetricExporter) Option {
	return func(c *config) { c.MetricExporter = exp }
}
