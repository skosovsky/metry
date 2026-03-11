package metry

import (
	"context"
	"errors"
	"fmt"
	"time"

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
// Resource includes service.name, service.version, deployment.environment and
// telemetry.sdk.* attributes (e.g. telemetry.sdk.language=go) for backend dashboards.
func Init(ctx context.Context, opts Options) (shutdown func(context.Context) error, err error) {
	if opts.ServiceName == "" {
		return nil, ErrServiceNameRequired
	}
	traceRatio := 1.0
	if opts.TraceRatio != nil {
		traceRatio = *opts.TraceRatio
	}

	// Build resource with service attributes and SDK semconv (e.g. telemetry.sdk.language=go).
	// Omit empty optional attributes so backends do not show empty values in UI.
	attrs := []attribute.KeyValue{
		semconv.ServiceName(opts.ServiceName),
		semconv.TelemetrySDKLanguageGo,
	}
	if opts.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(opts.ServiceVersion))
	}
	if opts.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(opts.Environment))
	}
	res := resource.NewWithAttributes(semconv.SchemaURL, attrs...)

	// Trace provider.
	var tp *sdktrace.TracerProvider
	{
		var exp sdktrace.SpanExporter
		if opts.TraceExporter != nil && opts.TraceExporter.create != nil {
			exp, err = opts.TraceExporter.create(ctx, res)
			if err != nil {
				return nil, fmt.Errorf("metry: create trace exporter: %w", err)
			}
		} else {
			exp = noopSpanExporter{}
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(exp),
			sdktrace.WithSampler(sdktrace.TraceIDRatioBased(traceRatio)),
		)
		otel.SetTracerProvider(tp)
	}

	// Metric provider (only if exporter provided).
	var mp *sdkmetric.MeterProvider
	if opts.MetricExporter != nil && opts.MetricExporter.create != nil {
		exp, err := opts.MetricExporter.create(ctx, res)
		if err != nil {
			_ = tp.Shutdown(ctx)
			return nil, fmt.Errorf("metry: create metric exporter: %w", err)
		}
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
		)
		otel.SetMeterProvider(mp)
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
