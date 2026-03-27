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
)

const shutdownTimeout = 10 * time.Second

var (
	// ErrServiceNameRequired is returned when New is called with an empty ServiceName.
	ErrServiceNameRequired = errors.New("metry: ServiceName is required")
)

// Provider is the stateless runtime object created by New.
// It exposes OTel providers and propagator without mutating global state.
type Provider struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator

	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider

	shutdownOnce sync.Once
	shutdownErr  error
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

	exp := cfg.Exporter
	if exp == nil {
		exp = noopSpanExporter{}
	}
	sampler := cfg.Sampler
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
	if cfg.MetricExporter != nil {
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(cfg.MetricExporter)),
		)
		activeMeter = mp
	}

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	return &Provider{
		TracerProvider: tp,
		MeterProvider:  activeMeter,
		Propagator:     propagator,
		tp:             tp,
		mp:             mp,
		shutdownOnce:   sync.Once{},
		shutdownErr:    nil,
	}, nil
}

// Shutdown releases resources owned by this Provider.
// Shutdown is idempotent.
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
