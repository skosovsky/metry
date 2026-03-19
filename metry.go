package metry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"

	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/internal/genaiconfig"
)

const (
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
	prevGenAIConfig := genaiconfig.Load()
	genAIConfigToken := genaiconfig.New(cfg.maxGenAIContextLength, cfg.RecordPayloads)
	genaiconfig.Store(genAIConfigToken)

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
	// Use resource.Default() until SDK provides resource.New(ctx, resource.WithDefault()) for context-aware init.
	defRes := resource.Default()
	res, err := resource.Merge(defRes, customRes)
	if err != nil {
		genaiconfig.CompareAndSwap(genAIConfigToken, prevGenAIConfig)
		return nil, fmt.Errorf("metry: merge resource: %w", err)
	}

	// Trace provider.
	var tp *sdktrace.TracerProvider
	{
		exp := cfg.Exporter
		if exp == nil {
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
	if cfg.MetricExporter != nil {
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(cfg.MetricExporter)),
		)
		otel.SetMeterProvider(mp)
		var regErr error
		cleanupGenAI, regErr = genai.RegisterMetricsForInit(otel.Meter(meterName))
		if regErr != nil {
			errs := []error{fmt.Errorf("metry: register genai metrics: %w", regErr)}
			if cleanupGenAI != nil {
				cleanupGenAI()
			}
			if e := tp.Shutdown(ctx); e != nil {
				errs = append(errs, e)
			}
			if e := mp.Shutdown(ctx); e != nil {
				errs = append(errs, e)
			}
			otel.SetTracerProvider(prevTracer)
			otel.SetMeterProvider(prevMeter)
			genaiconfig.CompareAndSwap(genAIConfigToken, prevGenAIConfig)
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
		genaiconfig.CompareAndSwap(genAIConfigToken, genaiconfig.Default())
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	}
	return shutdown, nil
}

// noopSpanExporter implements sdktrace.SpanExporter and drops all spans.
type noopSpanExporter struct{}

func (noopSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (noopSpanExporter) Shutdown(context.Context) error                             { return nil }
