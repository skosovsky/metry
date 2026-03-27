package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestNewHintSampler_KeepHintForcesSampling(t *testing.T) {
	sampler := NewHintSampler(sdktrace.NeverSample())

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		Attributes: []attribute.KeyValue{
			SamplingKeepKey.Bool(true),
		},
	})

	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func TestNewHintSampler_DelegatesWithoutKeepHint(t *testing.T) {
	always := NewHintSampler(sdktrace.AlwaysSample())
	alwaysResult := always.ShouldSample(sdktrace.SamplingParameters{})
	assert.Equal(t, sdktrace.RecordAndSample, alwaysResult.Decision)

	never := NewHintSampler(sdktrace.NeverSample())
	neverResult := never.ShouldSample(sdktrace.SamplingParameters{})
	assert.Equal(t, sdktrace.Drop, neverResult.Decision)
}

func TestNewHintSampler_NilBaseDefaultsToNeverSample(t *testing.T) {
	sampler := NewHintSampler(nil)
	result := sampler.ShouldSample(sdktrace.SamplingParameters{})

	assert.Equal(t, sdktrace.Drop, result.Decision)
}

func TestNewHintSampler_SampledParent_PropagatesSampleDecision(t *testing.T) {
	sampler := NewHintSampler(sdktrace.NeverSample())
	parent := sampledParentSpanContext()

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), parent),
	})

	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func TestNewHintSampler_UnsampledParent_PropagatesDropDecision(t *testing.T) {
	sampler := NewHintSampler(sdktrace.AlwaysSample())
	parent := unsampledParentSpanContext()

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), parent),
	})

	assert.Equal(t, sdktrace.Drop, result.Decision)
}

func TestNewHintSampler_KeepHint_OverridesUnsampledParent(t *testing.T) {
	sampler := NewHintSampler(sdktrace.NeverSample())
	parent := unsampledParentSpanContext()

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), parent),
		Attributes: []attribute.KeyValue{
			SamplingKeepKey.Bool(true),
		},
	})

	assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
}

func sampledParentSpanContext() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		SpanID:     trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
}

func unsampledParentSpanContext() trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3},
		SpanID:     trace.SpanID{4, 4, 4, 4, 4, 4, 4, 4},
		TraceFlags: 0,
		Remote:     true,
	})
}
