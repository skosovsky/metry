package metry

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/genaiwire"
	"github.com/skosovsky/metry/internal/metrytestwire"
)

func init() {
	genaiwire.AsyncSpanContext = func(value any) (trace.SpanContext, error) {
		handle, ok := value.(AsyncHandle)
		if !ok || !handle.IsValid() {
			return trace.SpanContext{}, ErrInvalidAsyncHandle
		}
		return handle.h.SpanContext(), nil
	}

	metrytestwire.NewProviderFromDeps = func(
		tracer trace.TracerProvider,
		meter metric.MeterProvider,
		prop propagation.TextMapPropagator,
	) any {
		return newProviderFromDeps(tracer, meter, prop)
	}
}
