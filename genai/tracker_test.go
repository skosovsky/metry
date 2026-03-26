package genai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewTracker_RequiresMeterAndTracer(t *testing.T) {
	_, err := NewTracker(nil, nil)
	require.ErrorIs(t, err, ErrMeterRequired)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	_, err = NewTracker(mp.Meter("tracker"), nil)
	require.ErrorIs(t, err, ErrTracerRequired)
}

func TestNewTracker_UsesExplicitTracerAndKeepsTrackersIsolated(t *testing.T) {
	reader1 := sdkmetric.NewManualReader()
	mp1 := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader1))
	t.Cleanup(func() { _ = mp1.Shutdown(context.Background()) })
	traceExporter1 := tracetest.NewInMemoryExporter()
	tp1 := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter1))
	t.Cleanup(func() { _ = tp1.Shutdown(context.Background()) })

	reader2 := sdkmetric.NewManualReader()
	mp2 := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader2))
	t.Cleanup(func() { _ = mp2.Shutdown(context.Background()) })
	traceExporter2 := tracetest.NewInMemoryExporter()
	tp2 := sdktrace.NewTracerProvider(sdktrace.WithSyncer(traceExporter2))
	t.Cleanup(func() { _ = tp2.Shutdown(context.Background()) })

	tracker1, err := NewTracker(
		mp1.Meter("tracker-1"),
		tp1.Tracer("tracker-1"),
		WithRecordPayloads(true),
		WithMaxContextLength(96),
	)
	require.NoError(t, err)
	tracker2, err := NewTracker(
		mp2.Meter("tracker-2"),
		tp2.Tracer("tracker-2"),
		WithRecordPayloads(false),
		WithMaxContextLength(32),
	)
	require.NoError(t, err)

	toolJSON := `{"prompt":"` + strings.Repeat("a", 512) + `"}`

	ctx1, span1 := tracker1.StartToolSpan(context.Background(), "search", "call-1", toolJSON)
	tracker1.RecordInteraction(ctx1, span1, testMeta(), testPayload(), Usage{
		InputTokens:           12,
		OutputTokens:          6,
		CacheReadInputTokens:  3,
		ReasoningOutputTokens: 2,
	})
	tracker1.RecordToolResult(span1, `{"result":"`+strings.Repeat("b", 512)+`"}`, false)
	span1.End()

	ctx2, span2 := tracker2.StartToolSpan(context.Background(), "search", "call-2", toolJSON)
	tracker2.RecordInteraction(ctx2, span2, testMeta(), testPayload(), Usage{
		InputTokens:  4,
		OutputTokens: 2,
	})
	tracker2.RecordToolResult(span2, `{"result":"ok"}`, false)
	span2.End()

	spans1 := traceExporter1.GetSpans()
	require.Len(t, spans1, 1)
	attrs1 := spanAttributes(spans1[0].Attributes)
	_, ok := attrs1[InputMessagesKey]
	require.True(t, ok)

	spans2 := traceExporter2.GetSpans()
	require.Len(t, spans2, 1)
	attrs2 := spanAttributes(spans2[0].Attributes)
	_, ok = attrs2[InputMessagesKey]
	require.False(t, ok)

	rm1 := collectMetrics(t, reader1)
	assert.InDelta(t, 12, int64HistogramSumByTokenType(t, rm1, TokenUsageMetricName, TokenTypeInput), 1e-9)
	assert.InDelta(t, 6, int64HistogramSumByTokenType(t, rm1, TokenUsageMetricName, TokenTypeOutput), 1e-9)
	assert.InDelta(
		t,
		3,
		int64HistogramSumByTokenType(t, rm1, TokenComponentUsageMetricName, TokenTypeInputCacheRead),
		1e-9,
	)
	assert.InDelta(
		t,
		2,
		int64HistogramSumByTokenType(t, rm1, TokenComponentUsageMetricName, TokenTypeOutputReasoning),
		1e-9,
	)

	rm2 := collectMetrics(t, reader2)
	assert.InDelta(t, 4, int64HistogramSumByTokenType(t, rm2, TokenUsageMetricName, TokenTypeInput), 1e-9)
	assert.InDelta(t, 2, int64HistogramSumByTokenType(t, rm2, TokenUsageMetricName, TokenTypeOutput), 1e-9)
	assertMetricAbsent(t, rm2, TokenComponentUsageMetricName)
}

func spanAttributes(attrs []attribute.KeyValue) map[attribute.Key]attribute.Value {
	set := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, attr := range attrs {
		set[attr.Key] = attr.Value
	}
	return set
}
