// Package http provides HTTP middleware for metry that creates root spans and propagates trace context.
package http

import (
	"net/http"

	"github.com/skosovsky/metry"
)

// Option configures HTTP middleware without exposing OpenTelemetry types.
type Option = metry.HTTPHandlerOption

// WithSpanNameFormatter sets a custom span name formatter.
//
//nolint:gochecknoglobals // thin alias to metry root API
var WithSpanNameFormatter = metry.WithHTTPSpanNameFormatter

// Handler wraps next with OpenTelemetry instrumentation via metry.HTTPHandler.
func Handler(provider *metry.Provider, next http.Handler, operationName string, opts ...Option) http.Handler {
	return metry.HTTPHandler(provider, next, operationName, opts...)
}
