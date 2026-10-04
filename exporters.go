package metry

import sdktrace "go.opentelemetry.io/otel/sdk/trace"

// NeverSample returns a sampler that drops all spans.
func NeverSample() sdktrace.Sampler { return sdktrace.NeverSample() }

// AlwaysSample returns a sampler that records and samples every span.
func AlwaysSample() sdktrace.Sampler { return sdktrace.AlwaysSample() }

// TraceIDRatioBased returns a ratio sampler suitable for new roots.
// Use the default provider sampler to inherit a parent's decision.
func TraceIDRatioBased(ratio float64) sdktrace.Sampler { return sdktrace.TraceIDRatioBased(ratio) }
