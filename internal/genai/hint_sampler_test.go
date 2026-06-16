package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func sampledParentSpanContext() trace.SpanContext {
	traceID := trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	spanID := trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
}

func unsampledParentSpanContext() trace.SpanContext {
	traceID := trace.TraceID{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3}
	spanID := trace.SpanID{4, 4, 4, 4, 4, 4, 4, 4}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: 0,
		Remote:     true,
	})
}

func TestNewHintSamplerSDK_KeepHintForcesSampling(t *testing.T) {
	sampler := NewHintSamplerSDK(sdktrace.NeverSample())

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		Attributes: []attribute.KeyValue{
			attribute.Key("gen_ai.sampling.keep").Bool(true),
		},
	})

	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func TestNewHintSamplerSDK_DelegatesWithoutKeepHint(t *testing.T) {
	always := NewHintSamplerSDK(sdktrace.AlwaysSample())
	alwaysResult := always.ShouldSample(sdktrace.SamplingParameters{})
	assert.Equal(t, sdktrace.RecordAndSample, alwaysResult.Decision)

	never := NewHintSamplerSDK(sdktrace.NeverSample())
	neverResult := never.ShouldSample(sdktrace.SamplingParameters{})
	assert.Equal(t, sdktrace.Drop, neverResult.Decision)
}

func TestNewHintSamplerSDK_NilBaseDefaultsToNeverSample(t *testing.T) {
	sampler := NewHintSamplerSDK(nil)
	result := sampler.ShouldSample(sdktrace.SamplingParameters{})

	assert.Equal(t, sdktrace.Drop, result.Decision)
}

func TestNewHintSamplerSDK_SampledParent_PropagatesSampleDecision(t *testing.T) {
	sampler := NewHintSamplerSDK(sdktrace.NeverSample())
	parent := sampledParentSpanContext()

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), parent),
	})

	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func TestNewHintSamplerSDK_UnsampledParent_PropagatesDropDecision(t *testing.T) {
	sampler := NewHintSamplerSDK(sdktrace.AlwaysSample())
	parent := unsampledParentSpanContext()

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), parent),
	})

	assert.Equal(t, sdktrace.Drop, result.Decision)
}

func TestNewHintSamplerSDK_KeepHint_OverridesUnsampledParent(t *testing.T) {
	sampler := NewHintSamplerSDK(sdktrace.NeverSample())
	parent := unsampledParentSpanContext()

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), parent),
		Attributes: []attribute.KeyValue{
			attribute.Key("gen_ai.sampling.keep").Bool(true),
		},
	})

	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}
