package metry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/skosovsky/metry/diagnostic"
	metryprop "github.com/skosovsky/metry/propagation"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

const shutdownTimeout = 10 * time.Second

var (
	// ErrServiceNameRequired is returned when New is called with an empty ServiceName.
	ErrServiceNameRequired = errors.New("metry: ServiceName is required")
)

// Provider owns instrumentation configuration, lazy instruments and lifecycle
// state. New owns its SDK pipeline; FromSDK borrows host-owned dependencies.
// A Provider must not be copied after first use.
type Provider struct {
	otelTracer        trace.TracerProvider
	otelMeter         metric.MeterProvider
	propagator        propagation.TextMapPropagator
	propagationPolicy metryprop.Policy
	diagnosticPolicy  diagnostic.Policy

	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider

	executorOnce    sync.Once
	executorMetrics *executorInstruments
	executorErr     error

	shutdownOnce sync.Once
	shutdownErr  error
}

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
		return metryprop.Wrap(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		), metryprop.Policy{})
	}
	return p.propagator
}

// New creates a metry runtime provider.
// The returned Provider does not install any global OTel state.
func New(ctx context.Context, opts ...Option) (*Provider, error) {
	_ = ctx // kept for forward-compatible constructor shape.

	cfg := newConfig()
	for _, o := range opts {
		if o == nil {
			return nil, ErrInvalidConfiguration
		}
		o(cfg)
	}
	if strings.TrimSpace(cfg.ServiceName) == "" {
		return nil, ErrServiceNameRequired
	}

	if cfg.invalid || math.IsNaN(cfg.TraceRatio) || math.IsInf(cfg.TraceRatio, 0) || cfg.TraceRatio < 0 ||
		cfg.TraceRatio > 1 {
		return nil, ErrInvalidConfiguration
	}

	customAttrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.TelemetrySDKLanguageGo,
	}
	if cfg.ServiceVersion != "" {
		customAttrs = append(customAttrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		customAttrs = append(customAttrs, semconv.DeploymentEnvironmentNameKey.String(cfg.Environment))
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
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceRatio))
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sampler),
	)

	activeMeter := metric.MeterProvider(noopmetric.NewMeterProvider())
	var mp *sdkmetric.MeterProvider
	if metricExp := cfg.MetricExporter; metricExp != nil {
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

	return &Provider{ //nolint:exhaustruct_v5 // executor instruments are initialized lazily.
		otelTracer:        tp,
		otelMeter:         activeMeter,
		propagator:        metryprop.Wrap(propagator, cfg.PropagationPolicy),
		propagationPolicy: cfg.PropagationPolicy,
		diagnosticPolicy:  cfg.DiagnosticPolicy,
		tp:                tp,
		mp:                mp,
		shutdownOnce:      sync.Once{},
		shutdownErr:       nil,
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

// Shutdown is terminal and idempotent for resources owned by this Provider.
// Concurrent callers wait and receive the same first result, including failure.
// The first context bounds shutdown by its deadline and a ten-second upper limit;
// an already cancelled first context is terminal, not a request to retry. Use a
// live shutdown context. Borrowed dependencies remain under host ownership.
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

// DiagnosticPolicy returns the immutable policy used for library-owned diagnostic text.
// Nil and zero providers use the safe default; custom span attrs are host-owned.
func (p *Provider) DiagnosticPolicy() diagnostic.Policy {
	if p == nil {
		return diagnostic.Policy{}
	}
	return p.diagnosticPolicy
}
