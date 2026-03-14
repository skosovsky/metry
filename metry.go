package metry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/skosovsky/metry/genai"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName      = "metry"
	meterName       = "metry"
	shutdownTimeout = 10 * time.Second
)

var (
	// ErrServiceNameRequired is returned when Init is called with an empty ServiceName.
	ErrServiceNameRequired = errors.New("metry: ServiceName is required")
)

// Init configures global OTel providers and returns a shutdown function.
// Uses resource.Default() merged with service attributes for host/PID/OS and service identity.
// On partial failure, already-created providers are shut down and global otel state is
// restored to the previous tracer/meter (atomic "all or nothing" for global provider state).
func Init(ctx context.Context, opts ...Option) (shutdown func(context.Context) error, err error) {
	prevTracer := otel.GetTracerProvider()
	prevMeter := otel.GetMeterProvider()

	cfg := newConfig()
	for _, o := range opts {
		o(cfg)
	}
	if cfg.ServiceName == "" {
		return nil, ErrServiceNameRequired
	}

	// Custom resource with service attributes; merge with default (host, PID, OS).
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

	// Trace provider.
	var tp *sdktrace.TracerProvider
	{
		var exp sdktrace.SpanExporter
		if cfg.TraceExporter != nil && cfg.TraceExporter.create != nil {
			exp, err = cfg.TraceExporter.create(ctx, res)
			if err != nil {
				return nil, fmt.Errorf("metry: create trace exporter: %w", err)
			}
		} else {
			exp = noopSpanExporter{}
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(exp),
			sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.TraceRatio)),
		)
		otel.SetTracerProvider(tp)
	}

	// Metric provider (only if exporter provided). On failure, rollback trace provider and restore globals.
	var mp *sdkmetric.MeterProvider
	var cleanupGenAI func()
	if cfg.MetricExporter != nil && cfg.MetricExporter.create != nil {
		exp, createErr := cfg.MetricExporter.create(ctx, res)
		if createErr != nil {
			_ = tp.Shutdown(ctx)
			otel.SetTracerProvider(prevTracer)
			return nil, errors.Join(fmt.Errorf("metry: create metric exporter: %w", createErr))
		}
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
		)
		otel.SetMeterProvider(mp)
		var regErr error
		cleanupGenAI, regErr = genai.RegisterMetrics(otel.Meter(meterName))
		if regErr != nil {
			errs := []error{fmt.Errorf("metry: register genai metrics: %w", regErr)}
			cleanupGenAI()
			if e := tp.Shutdown(ctx); e != nil {
				errs = append(errs, e)
			}
			if e := mp.Shutdown(ctx); e != nil {
				errs = append(errs, e)
			}
			otel.SetTracerProvider(prevTracer)
			otel.SetMeterProvider(prevMeter)
			return nil, errors.Join(errs...)
		}
	}

	// W3C Trace Context and Baggage propagation.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown = func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		var errs []error
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer shutdown: %w", err))
		}
		if mp != nil {
			if cleanupGenAI != nil {
				cleanupGenAI()
			}
			if err := mp.Shutdown(ctx); err != nil {
				errs = append(errs, fmt.Errorf("meter shutdown: %w", err))
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}
	return shutdown, nil
}

// GlobalTracer returns the global Tracer for the library (name "metry").
// Call after Init.
func GlobalTracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// GlobalMeter returns the global Meter for the library (name "metry").
// Call after Init.
func GlobalMeter() metric.Meter {
	return otel.Meter(meterName)
}

// noopSpanExporter implements sdktrace.SpanExporter and drops all spans.
type noopSpanExporter struct{}

func (noopSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (noopSpanExporter) Shutdown(context.Context) error                             { return nil }
