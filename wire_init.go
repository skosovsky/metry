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
	genaiwire.MeterTracer = func(p any) (metric.Meter, trace.Tracer, error) {
		provider, ok := p.(*Provider)
		if !ok {
			return nil, nil, ErrInvalidProviderType
		}
		if provider == nil {
			return nil, nil, ErrNilProvider
		}
		if provider.otelMeter == nil {
			return nil, nil, ErrNilMeterProvider
		}
		if provider.otelTracer == nil {
			return nil, nil, ErrNilTracerProvider
		}
		return provider.otelMeter.Meter(genaiMeterName), provider.otelTracer.Tracer(genaiTracerName), nil
	}

	metrytestwire.NewProviderFromDeps = func(
		tracer trace.TracerProvider,
		meter metric.MeterProvider,
		prop propagation.TextMapPropagator,
	) any {
		return newProviderFromDeps(tracer, meter, prop)
	}
}
