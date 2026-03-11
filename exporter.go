package metry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// OTLPGRPC returns trace and metric exporters that send data via OTLP over gRPC.
// endpoint is the target host:port (e.g. "localhost:4317"). If insecure is true, TLS is disabled.
func OTLPGRPC(endpoint string, insecure bool) (*TraceExporter, *MetricExporter) {
	te := &TraceExporter{
		create: func(ctx context.Context, _ *resource.Resource) (sdktrace.SpanExporter, error) {
			opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
			if insecure {
				opts = append(opts, otlptracegrpc.WithInsecure())
			}
			return otlptracegrpc.New(ctx, opts...)
		},
	}
	me := &MetricExporter{
		create: func(ctx context.Context, _ *resource.Resource) (sdkmetric.Exporter, error) {
			opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
			if insecure {
				opts = append(opts, otlpmetricgrpc.WithInsecure())
			}
			return otlpmetricgrpc.New(ctx, opts...)
		},
	}
	return te, me
}

// OTLPHTTP returns trace and metric exporters that send data via OTLP over HTTP.
// endpoint is the target host:port (e.g. "localhost:4318"). headers are optional HTTP headers.
func OTLPHTTP(endpoint string, headers map[string]string) (*TraceExporter, *MetricExporter) {
	te := &TraceExporter{
		create: func(ctx context.Context, _ *resource.Resource) (sdktrace.SpanExporter, error) {
			opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
			if len(headers) > 0 {
				opts = append(opts, otlptracehttp.WithHeaders(headers))
			}
			return otlptracehttp.New(ctx, opts...)
		},
	}
	me := &MetricExporter{
		create: func(ctx context.Context, _ *resource.Resource) (sdkmetric.Exporter, error) {
			opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(endpoint)}
			if len(headers) > 0 {
				opts = append(opts, otlpmetrichttp.WithHeaders(headers))
			}
			return otlpmetrichttp.New(ctx, opts...)
		},
	}
	return te, me
}

// Console returns trace and metric exporters that write to stdout (for local development).
func Console() (*TraceExporter, *MetricExporter) {
	te := &TraceExporter{
		create: func(_ context.Context, _ *resource.Resource) (sdktrace.SpanExporter, error) {
			return stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
		},
	}
	me := &MetricExporter{
		create: func(_ context.Context, _ *resource.Resource) (sdkmetric.Exporter, error) {
			return stdoutmetric.New(stdoutmetric.WithWriter(os.Stdout))
		},
	}
	return te, me
}

// Noop returns trace and metric exporters that drop all data (for disabling telemetry or tests).
func Noop() (*TraceExporter, *MetricExporter) {
	te := &TraceExporter{
		create: func(_ context.Context, _ *resource.Resource) (sdktrace.SpanExporter, error) {
			return noopSpanExporter{}, nil
		},
	}
	me := &MetricExporter{
		create: func(_ context.Context, _ *resource.Resource) (sdkmetric.Exporter, error) {
			return noopMetricExporter{}, nil
		},
	}
	return te, me
}

// noopMetricExporter implements sdkmetric.Exporter and drops all metrics.
type noopMetricExporter struct{}

func (noopMetricExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(k)
}
func (noopMetricExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}
func (noopMetricExporter) Export(_ context.Context, _ *metricdata.ResourceMetrics) error { return nil }
func (noopMetricExporter) ForceFlush(context.Context) error                              { return nil }
func (noopMetricExporter) Shutdown(context.Context) error                                { return nil }
