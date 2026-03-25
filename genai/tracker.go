package genai

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/skosovsky/metry/internal/defaultslot"
)

// Tracker owns GenAI runtime config, metric instruments, and the tracer used for tool spans.
type Tracker struct {
	cfg     runtimeConfig
	metrics *metricsHolder
	tracer  trace.Tracer
}

type metricsHolder struct {
	TokenUsage          metric.Int64Histogram
	TokenComponentUsage metric.Int64Histogram
	OperationDuration   metric.Float64Histogram
	Cost                metric.Float64Counter
	TTFT                metric.Float64Histogram
	TPS                 metric.Float64Histogram
	TBT                 metric.Float64Histogram
	VideoSeconds        metric.Float64Histogram
	VideoFrames         metric.Int64Histogram
}

//nolint:gochecknoglobals // Default() returns this when no tracker is installed in the slot.
var noopTracker = &Tracker{
	cfg:     defaultRuntimeConfig(),
	metrics: nil,
	tracer:  noop.NewTracerProvider().Tracer(tracerName),
}

// NewTracker creates an explicit GenAI tracker with its own config and instruments.
func NewTracker(meter metric.Meter, opts ...Option) (*Tracker, error) {
	return NewTrackerWithTracer(meter, otel.Tracer(tracerName), opts...)
}

// NewTrackerWithTracer creates an explicit GenAI tracker with its own config, instruments, and tracer.
func NewTrackerWithTracer(meter metric.Meter, tracer trace.Tracer, opts ...Option) (*Tracker, error) {
	cfg := buildRuntimeConfig(opts...)
	holder, err := newMetricsHolder(meter)
	if err != nil {
		return nil, err
	}
	if tracer == nil {
		tracer = otel.Tracer(tracerName)
	}
	return &Tracker{
		cfg:     cfg,
		metrics: holder,
		tracer:  tracer,
	}, nil
}

// Default returns the package-level default tracker.
func Default() *Tracker {
	if tracker, ok := defaultslot.Load().(*Tracker); ok && tracker != nil {
		return tracker
	}
	return noopTracker
}

//nolint:funlen // Sequential OTel instrument registration; splitting would obscure error handling.
func newMetricsHolder(meter metric.Meter) (*metricsHolder, error) {
	if meter == nil {
		return nil, nil //nolint:nilnil // nil meter is supported: tracker records spans without metric instruments.
	}

	tokenUsage, err := meter.Int64Histogram(
		TokenUsageMetricName,
		metric.WithUnit("{token}"),
		metric.WithDescription("Number of GenAI tokens used per operation."),
		metric.WithExplicitBucketBoundaries(
			1,
			4,
			16,
			64,
			256,
			1024,
			4096,
			16384,
			65536,
			262144,
			1048576,
			4194304,
			16777216,
			67108864,
		),
	)
	if err != nil {
		return nil, err
	}
	tokenComponentUsage, err := meter.Int64Histogram(
		TokenComponentUsageMetricName,
		metric.WithUnit("{token}"),
		metric.WithDescription("Custom GenAI token component usage per operation."),
		metric.WithExplicitBucketBoundaries(
			1,
			4,
			16,
			64,
			256,
			1024,
			4096,
			16384,
			65536,
			262144,
			1048576,
			4194304,
			16777216,
			67108864,
		),
	)
	if err != nil {
		return nil, err
	}
	operationDuration, err := meter.Float64Histogram(
		OperationDurationMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("GenAI client operation duration."),
		metric.WithExplicitBucketBoundaries(
			0.01,
			0.02,
			0.04,
			0.08,
			0.16,
			0.32,
			0.64,
			1.28,
			2.56,
			5.12,
			10.24,
			20.48,
			40.96,
			81.92,
		),
	)
	if err != nil {
		return nil, err
	}
	cost, err := meter.Float64Counter(
		CostMetricName,
		metric.WithDescription("Custom GenAI cost counter."),
	)
	if err != nil {
		return nil, err
	}
	ttft, err := meter.Float64Histogram(
		TTFTMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Client-observed time to first token."),
		metric.WithExplicitBucketBoundaries(
			0.001,
			0.005,
			0.01,
			0.02,
			0.04,
			0.06,
			0.08,
			0.1,
			0.25,
			0.5,
			0.75,
			1.0,
			2.5,
			5.0,
			7.5,
			10.0,
		),
	)
	if err != nil {
		return nil, err
	}
	tps, err := meter.Float64Histogram(
		StreamingTPSMetricName,
		metric.WithUnit("token/s"),
		metric.WithDescription("Client-observed tokens per second during streaming generation."),
		metric.WithExplicitBucketBoundaries(0.25, 0.5, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512),
	)
	if err != nil {
		return nil, err
	}
	tbt, err := meter.Float64Histogram(
		StreamingTBTMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Client-observed time between output tokens."),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.2, 0.3, 0.4, 0.5, 0.75, 1.0, 2.5),
	)
	if err != nil {
		return nil, err
	}
	videoSeconds, err := meter.Float64Histogram(
		VideoSecondsMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Custom video duration per GenAI interaction."),
		metric.WithExplicitBucketBoundaries(0.25, 0.5, 1, 2, 4, 8, 16, 32, 64, 128, 256),
	)
	if err != nil {
		return nil, err
	}
	videoFrames, err := meter.Int64Histogram(
		VideoFramesMetricName,
		metric.WithUnit("{frame}"),
		metric.WithDescription("Custom video frame count per GenAI interaction."),
		metric.WithExplicitBucketBoundaries(1, 4, 16, 64, 256, 1024, 4096, 16384),
	)
	if err != nil {
		return nil, err
	}

	return &metricsHolder{
		TokenUsage:          tokenUsage,
		TokenComponentUsage: tokenComponentUsage,
		OperationDuration:   operationDuration,
		Cost:                cost,
		TTFT:                ttft,
		TPS:                 tps,
		TBT:                 tbt,
		VideoSeconds:        videoSeconds,
		VideoFrames:         videoFrames,
	}, nil
}
