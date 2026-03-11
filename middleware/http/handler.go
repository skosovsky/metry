// Package http provides HTTP middleware for metry that creates root spans and propagates trace context.
//
//nolint:revive // package name "http" is intentional for HTTP middleware domain
package http

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Handler wraps next with OpenTelemetry instrumentation: a root span is created
// for each request (or the incoming trace context is extracted from W3C headers),
// and the span is named after operationName. Use after metry.Init so the global
// TracerProvider and propagators are set.
func Handler(next http.Handler, operationName string) http.Handler {
	return otelhttp.NewHandler(next, operationName)
}
