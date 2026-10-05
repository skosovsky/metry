package metry

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// HTTPHandlerOption configures HTTPHandler without exposing OpenTelemetry types.
type HTTPHandlerOption func(*httpHandlerConfig)

type httpHandlerConfig struct {
	otelOpts []otelhttp.Option
}

// WithHTTPSpanNameFormatter sets a custom span name formatter for HTTPHandler.
func WithHTTPSpanNameFormatter(fn func(operation string, r *http.Request) string) HTTPHandlerOption {
	return func(c *httpHandlerConfig) {
		c.otelOpts = append(c.otelOpts, otelhttp.WithSpanNameFormatter(fn))
	}
}

func httpHandlerOptionsToOTel(opts []HTTPHandlerOption) []otelhttp.Option {
	var cfg httpHandlerConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg.otelOpts
}

// HTTPHandler wraps next with OpenTelemetry instrumentation using provider OTel deps.
func HTTPHandler(provider *Provider, next http.Handler, operationName string, opts ...HTTPHandlerOption) http.Handler {
	if provider == nil {
		panic("metry: provider is required")
	}

	base := []otelhttp.Option{
		otelhttp.WithTracerProvider(provider.tracerProvider()),
		otelhttp.WithMeterProvider(provider.meterProvider()),
		otelhttp.WithPropagators(provider.textMapPropagator()),
		otelhttp.WithSpanNameFormatter(func(operation string, _ *http.Request) string {
			return operation
		}),
	}
	all := base
	all = append(all, httpHandlerOptionsToOTel(opts)...)
	guarded := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				span := trace.SpanFromContext(request.Context())
				traceutil.RecordDiagnostic(span, provider.DiagnosticPolicy().Panic(value))
				// End before rethrow: SDK deferred End would otherwise format the original panic.
				span.End()
				panic(value)
			}
		}()
		next.ServeHTTP(w, request)
	})
	return otelhttp.NewHandler(guarded, operationName, all...)
}
