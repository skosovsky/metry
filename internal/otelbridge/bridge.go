// Package otelbridge wraps OpenTelemetry SDK types for use inside metry.
package otelbridge

import (
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TraceSampler holds an SDK trace sampler.
type TraceSampler struct {
	Sampler sdktrace.Sampler
}

// NeverSample returns a sampler that drops all spans.
func NeverSample() TraceSampler {
	return TraceSampler{Sampler: sdktrace.NeverSample()}
}

// AlwaysSample returns a sampler that records and samples every span.
func AlwaysSample() TraceSampler {
	return TraceSampler{Sampler: sdktrace.AlwaysSample()}
}

// TraceIDRatioBased returns a sampler that samples a fraction of traces by trace ID.
func TraceIDRatioBased(ratio float64) TraceSampler {
	return TraceSampler{Sampler: sdktrace.TraceIDRatioBased(ratio)}
}

// WrapTraceSampler wraps an SDK sampler.
func WrapTraceSampler(s sdktrace.Sampler) TraceSampler {
	if s == nil {
		return NeverSample()
	}
	return TraceSampler{Sampler: s}
}

// TraceSamplerSDK returns the underlying SDK sampler.
func TraceSamplerSDK(s TraceSampler) sdktrace.Sampler {
	if s.Sampler == nil {
		return sdktrace.NeverSample()
	}
	return s.Sampler
}

// SpanExporter holds an SDK span exporter.
type SpanExporter struct {
	Exp sdktrace.SpanExporter
}

// SpanExporterFromSDK wraps an SDK span exporter.
func SpanExporterFromSDK(exp sdktrace.SpanExporter) SpanExporter {
	return SpanExporter{Exp: exp}
}

// SpanExporterSDK returns the underlying SDK span exporter.
func SpanExporterSDK(e SpanExporter) sdktrace.SpanExporter {
	return e.Exp
}

// MetricExporter holds an SDK metric exporter.
type MetricExporter struct {
	Exp sdkmetric.Exporter
}

// MetricExporterFromSDK wraps an SDK metric exporter.
func MetricExporterFromSDK(exp sdkmetric.Exporter) MetricExporter {
	return MetricExporter{Exp: exp}
}

// MetricExporterSDK returns the underlying SDK metric exporter.
func MetricExporterSDK(e MetricExporter) sdkmetric.Exporter {
	return e.Exp
}
