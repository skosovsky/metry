package metry

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/otelbridge"
)

// TraceSampler decides whether a span should be recorded and sampled.
type TraceSampler struct {
	b otelbridge.TraceSampler
}

// NeverSample returns a sampler that drops all spans.
func NeverSample() TraceSampler {
	return TraceSampler{b: otelbridge.NeverSample()}
}

// AlwaysSample returns a sampler that records and samples every span.
func AlwaysSample() TraceSampler {
	return TraceSampler{b: otelbridge.AlwaysSample()}
}

// TraceIDRatioBased returns a sampler that samples a fraction of traces by trace ID.
func TraceIDRatioBased(ratio float64) TraceSampler {
	return TraceSampler{b: otelbridge.TraceIDRatioBased(ratio)}
}

func wrapTraceSampler(b otelbridge.TraceSampler) TraceSampler {
	return TraceSampler{b: b}
}

func traceSamplerSDK(s TraceSampler) sdktrace.Sampler {
	if s.b.Sampler == nil {
		return nil
	}
	return otelbridge.TraceSamplerSDK(s.b)
}

// SpanExporter exports finished spans.
type SpanExporter struct {
	b otelbridge.SpanExporter
}

func wrapSpanExporter(b otelbridge.SpanExporter) SpanExporter {
	return SpanExporter{b: b}
}

func spanExporterSDK(e SpanExporter) sdktrace.SpanExporter {
	return otelbridge.SpanExporterSDK(e.b)
}

// MetricExporter exports metric data.
type MetricExporter struct {
	b otelbridge.MetricExporter
}

func wrapMetricExporter(b otelbridge.MetricExporter) MetricExporter {
	return MetricExporter{b: b}
}

func metricExporterSDK(e MetricExporter) sdkmetric.Exporter {
	return otelbridge.MetricExporterSDK(e.b)
}

func newProviderFromDeps(
	tracer trace.TracerProvider,
	meter metric.MeterProvider,
	prop propagation.TextMapPropagator,
) *Provider {
	if prop == nil {
		prop = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}
	p := &Provider{ //nolint:exhaustruct // test-only partial provider
		otelTracer: tracer,
		otelMeter:  meter,
		propagator: prop,
	}
	if tp, ok := tracer.(*sdktrace.TracerProvider); ok {
		p.tp = tp
	}
	if mp, ok := meter.(*sdkmetric.MeterProvider); ok {
		p.mp = mp
	}
	return p
}
