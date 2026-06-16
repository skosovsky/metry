package metry

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	internalgenai "github.com/skosovsky/metry/internal/genai"
	"github.com/skosovsky/metry/internal/genaiwire"
	"github.com/skosovsky/metry/internal/metrytestwire"
	"github.com/skosovsky/metry/internal/otelbridge"
)

func init() {
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

	genaiwire.NewHintSampler = func(base any) any {
		sampler, ok := base.(TraceSampler)
		if !ok {
			panic("metry: genaiwire NewHintSampler expected TraceSampler")
		}
		sdk := traceSamplerSDK(sampler)
		wrapped := internalgenai.NewHintSamplerSDK(sdk)
		return wrapTraceSampler(otelbridge.WrapTraceSampler(wrapped))
	}

	metrytestwire.SpanExporter = func(exp sdktrace.SpanExporter) any {
		return wrapSpanExporter(otelbridge.SpanExporterFromSDK(exp))
	}
	metrytestwire.MetricExporter = func(exp sdkmetric.Exporter) any {
		return wrapMetricExporter(otelbridge.MetricExporterFromSDK(exp))
	}
	metrytestwire.NewProviderFromDeps = func(
		tracer trace.TracerProvider,
		meter metric.MeterProvider,
		prop propagation.TextMapPropagator,
	) any {
		return newProviderFromDeps(tracer, meter, prop)
	}
}
