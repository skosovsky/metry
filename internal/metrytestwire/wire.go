// Package metrytestwire exposes test-only provider construction hooks for metrytest.
package metrytestwire

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// NewProviderFromDeps constructs a provider with explicit OTel dependencies.
var NewProviderFromDeps func(
	tracer trace.TracerProvider,
	meter metric.MeterProvider,
	prop propagation.TextMapPropagator,
) any
