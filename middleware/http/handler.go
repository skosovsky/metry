// Package http provides HTTP middleware for metry that creates root spans and propagates trace context.
package http

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/skosovsky/metry"
)

// Handler wraps next with OpenTelemetry instrumentation: a root span is created
// for each request (or the incoming trace context is extracted from W3C headers),
// and the span is named after operationName using explicit provider dependencies.
func Handler(provider *metry.Provider, next http.Handler, operationName string) http.Handler {
	if provider == nil {
		panic("metry/http: provider is required")
	}

	return otelhttp.NewHandler(
		next,
		operationName,
		otelhttp.WithTracerProvider(provider.TracerProvider),
		otelhttp.WithMeterProvider(provider.MeterProvider),
		otelhttp.WithPropagators(provider.Propagator),
	)
}
