// Package metrytestwire exposes test-only provider construction hooks for metrytest.
package metrytestwire

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// SpanExporter builds a metry span exporter from an SDK exporter.
var SpanExporter func(sdktrace.SpanExporter) any

// MetricExporter builds a metry metric exporter from an SDK exporter.
var MetricExporter func(sdkmetric.Exporter) any

// NewProviderFromDeps constructs a provider with explicit OTel dependencies.
var NewProviderFromDeps func(
	tracer trace.TracerProvider,
	meter metric.MeterProvider,
	prop propagation.TextMapPropagator,
) any
