package metry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

const shutdownTimeout = 10 * time.Second

var (
	// ErrServiceNameRequired is returned when New is called with an empty ServiceName.
	ErrServiceNameRequired = errors.New("metry: ServiceName is required")
)

// Provider is the stateless runtime object created by New.
type Provider struct {
	otelTracer trace.TracerProvider
	otelMeter  metric.MeterProvider
	propagator propagation.TextMapPropagator

	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider

	shutdownOnce sync.Once
	shutdownErr  error
}

const (
	genaiMeterName  = "metry/genai"
	genaiTracerName = "metry/genai"
)

func (p *Provider) tracerProvider() trace.TracerProvider {
	if p == nil || p.otelTracer == nil {
		return nooptrace.NewTracerProvider()
	}
	return p.otelTracer
}

func (p *Provider) meterProvider() metric.MeterProvider {
	if p == nil || p.otelMeter == nil {
		return noopmetric.NewMeterProvider()
	}
	return p.otelMeter
}

func (p *Provider) textMapPropagator() propagation.TextMapPropagator {
	if p == nil || p.propagator == nil {
		return propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}
	return p.propagator
}

// New creates a metry runtime provider.
// The returned Provider does not install any global OTel state.
func New(ctx context.Context, opts ...Option) (*Provider, error) {
	_ = ctx // kept for forward-compatible constructor shape.

	cfg := newConfig()
	for _, o := range opts {
		o(cfg)
	}
	if cfg.ServiceName == "" {
		return nil, ErrServiceNameRequired
	}

	customAttrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.TelemetrySDKLanguageGo,
	}
	if cfg.ServiceVersion != "" {
		customAttrs = append(customAttrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		customAttrs = append(customAttrs, semconv.DeploymentEnvironmentName(cfg.Environment))
	}
	customRes := resource.NewWithAttributes(semconv.SchemaURL, customAttrs...)
	defRes := resource.Default()
	res, err := resource.Merge(defRes, customRes)
	if err != nil {
		return nil, fmt.Errorf("metry: merge resource: %w", err)
	}

	exp := spanExporterSDK(cfg.Exporter)
	if exp == nil {
		exp = noopSpanExporter{}
	}
	sampler := traceSamplerSDK(cfg.Sampler)
	if sampler == nil {
		sampler = sdktrace.TraceIDRatioBased(cfg.TraceRatio)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sampler),
	)

	activeMeter := metric.MeterProvider(noopmetric.NewMeterProvider())
	var mp *sdkmetric.MeterProvider
	if metricExp := metricExporterSDK(cfg.MetricExporter); metricExp != nil {
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		)
		activeMeter = mp
	}

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	return &Provider{
		otelTracer:   tp,
		otelMeter:    activeMeter,
		propagator:   propagator,
		tp:           tp,
		mp:           mp,
		shutdownOnce: sync.Once{},
		shutdownErr:  nil,
	}, nil
}

// ForceFlush flushes pending trace and metric data.
func (p *Provider) ForceFlush(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var errs []error
	if p.tp != nil {
		if err := p.tp.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer force flush: %w", err))
		}
	}
	if p.mp != nil {
		if err := p.mp.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter force flush: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Shutdown releases resources owned by this Provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}

	p.shutdownOnce.Do(func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		var errs []error
		if p.tp != nil {
			if err := p.tp.Shutdown(shutdownCtx); err != nil {
				errs = append(errs, fmt.Errorf("tracer shutdown: %w", err))
			}
		}
		if p.mp != nil {
			if err := p.mp.Shutdown(shutdownCtx); err != nil {
				errs = append(errs, fmt.Errorf("meter shutdown: %w", err))
			}
		}
		if len(errs) > 0 {
			p.shutdownErr = errors.Join(errs...)
		}
	})

	return p.shutdownErr
}

// noopSpanExporter implements sdktrace.SpanExporter and drops all spans.
type noopSpanExporter struct{}

func (noopSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (noopSpanExporter) Shutdown(context.Context) error                             { return nil }
