package genai

import (
	"errors"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/genaiwire"
)

var (
	// ErrMeterRequired is returned when NewTrackerFromProvider is called with a provider without a meter.
	ErrMeterRequired = errors.New("genai: meter is required")
	// ErrTracerRequired is returned when NewTrackerFromProvider is called with a provider without a tracer.
	ErrTracerRequired = errors.New("genai: tracer is required")
	// ErrProviderRequired is returned when NewTrackerFromProvider is called with a nil provider.
	ErrProviderRequired = errors.New("genai: provider is required")
)

// Tracker owns GenAI runtime config, metric instruments, and the tracer used for tool spans.
type Tracker struct {
	dimensions *metricDimensions
	cfg        runtimeConfig
	metrics    *metricsHolder
	tracer     trace.Tracer
	provider   *metry.Provider
}

type metricsHolder struct {
	InputTokenUsage                metric.Int64Histogram
	OutputTokenUsage               metric.Int64Histogram
	TokenComponentUsage            metric.Int64Histogram
	OperationDuration              metric.Float64Histogram
	ObservedOperationDuration      metric.Float64Histogram
	InvocationDuration             metric.Float64Histogram
	Cost                           metric.Float64Counter
	TTFT                           metric.Float64Histogram
	DerivedOutputTokensPerSecond   metric.Float64Histogram
	DerivedMeanOutputTokenInterval metric.Float64Histogram
	VideoSeconds                   metric.Float64Histogram
	VideoFrames                    metric.Int64Histogram
	ToolDuration                   metric.Float64Histogram
}

// NewTrackerFromProvider creates a GenAI tracker using the provider meter and tracer.
func NewTrackerFromProvider(p *metry.Provider, opts ...Option) (*Tracker, error) {
	if p == nil {
		return nil, ErrProviderRequired
	}
	cfg := buildRuntimeConfig(opts...)
	if configErr := validateRuntimeConfig(cfg); configErr != nil {
		return nil, configErr
	}
	meter, tracer, err := genaiwire.MeterTracer(p)
	if err != nil {
		if errors.Is(err, metry.ErrNilMeterProvider) {
			return nil, ErrMeterRequired
		}
		if errors.Is(err, metry.ErrNilTracerProvider) {
			return nil, ErrTracerRequired
		}
		return nil, err
	}

	holder, err := newMetricsHolder(meter)
	if err != nil {
		return nil, err
	}

	return &Tracker{
		dimensions: newMetricDimensions(cfg.profile),
		cfg:        cfg,
		metrics:    holder,
		tracer:     tracer,
		provider:   p,
	}, nil
}

func validatePolicyDeclaration(cfg runtimeConfig) error {
	var identity string
	switch cfg.PayloadPolicy().(type) {
	case rawPayloadPolicy:
		identity = profileRawPolicy
	case hashPayloadPolicy:
		identity = profileHashPolicy
	case redactPayloadPolicy:
		identity = profileRedactPolicy
	default:
		// Custom policies are declared and implemented by the host. Their
		// semantics cannot be inferred through the PayloadPolicy interface.
		return nil
	}
	if cfg.profile.PayloadPolicyID != identity {
		return invalidObservation("policy", "declaration_mismatch")
	}
	return nil
}

//nolint:funlen // Sequential OTel instrument registration; splitting would obscure error handling.
func newMetricsHolder(meter metric.Meter) (*metricsHolder, error) {
	tokenUsage, err := meter.Int64Histogram(
		InputTokenUsageMetricName,
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
	outputTokenUsage, err := meter.Int64Histogram(
		OutputTokenUsageMetricName,
		metric.WithUnit("{token}"),
		metric.WithDescription("Output tokens used per inference operation."),
	)
	if err != nil {
		return nil, err
	}
	observedOperationDuration, err := meter.Float64Histogram(
		ObservedOperationDurationMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Local or completed model observation duration."),
	)
	if err != nil {
		return nil, err
	}
	invocationDuration, err := meter.Float64Histogram(
		InvocationDurationMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Caller-bounded agent or workflow invocation duration."),
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
	derivedOutputTokensPerSecond, err := meter.Float64Histogram(
		DerivedOutputTokensPerSecondMetricName,
		metric.WithUnit("{token}/s"),
		metric.WithDescription(
			"Derived output token rate: output_tokens / (duration - TTFT), not measured chunk throughput.",
		),
		metric.WithExplicitBucketBoundaries(0.25, 0.5, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512),
	)
	if err != nil {
		return nil, err
	}
	derivedMeanOutputTokenInterval, err := meter.Float64Histogram(
		DerivedMeanOutputTokenIntervalMetricName,
		metric.WithUnit("s"),
		metric.WithDescription(
			"Derived mean output token interval: (duration - TTFT) / (output_tokens - 1), not measured inter-chunk latency.",
		),
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
	toolDuration, err := meter.Float64Histogram(
		ToolDurationMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("Tool execution duration observed by metry."),
		metric.WithExplicitBucketBoundaries(
			0.001,
			0.005,
			0.01,
			0.025,
			0.05,
			0.1,
			0.25,
			0.5,
			1,
			2.5,
			5,
			10,
			30,
			60,
		),
	)
	if err != nil {
		return nil, err
	}

	return &metricsHolder{
		InputTokenUsage:                tokenUsage,
		OutputTokenUsage:               outputTokenUsage,
		ObservedOperationDuration:      observedOperationDuration,
		InvocationDuration:             invocationDuration,
		TokenComponentUsage:            tokenComponentUsage,
		OperationDuration:              operationDuration,
		Cost:                           cost,
		TTFT:                           ttft,
		DerivedOutputTokensPerSecond:   derivedOutputTokensPerSecond,
		DerivedMeanOutputTokenInterval: derivedMeanOutputTokenInterval,
		VideoSeconds:                   videoSeconds,
		VideoFrames:                    videoFrames,
		ToolDuration:                   toolDuration,
	}, nil
}
