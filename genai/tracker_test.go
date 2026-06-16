package genai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestNewTrackerFromProvider_RequiresProvider(t *testing.T) {
	_, err := NewTrackerFromProvider(nil)
	require.ErrorIs(t, err, ErrProviderRequired)

	provider, _ := metrytest.NewTestProvider(t, metry.WithServiceName("genai-test"))
	tracker, err := NewTrackerFromProvider(provider)
	require.NoError(t, err)
	require.NotNil(t, tracker)
}

func TestNewTrackerFromProvider_KeepsTrackersIsolated(t *testing.T) {
	memMetric1 := testutil.NewInMemoryMetricExporter()
	memTrace1 := testutil.NewInMemoryTraceExporter()
	provider1, err := metry.New(context.Background(),
		metry.WithServiceName("tracker-1"),
		metry.WithExporter(metrytest.MetrySpanExporter(memTrace1)),
		metry.WithMetricExporter(metrytest.MetryMetricExporter(memMetric1)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider1.Shutdown(context.Background()) })

	memMetric2 := testutil.NewInMemoryMetricExporter()
	memTrace2 := testutil.NewInMemoryTraceExporter()
	provider2, err := metry.New(context.Background(),
		metry.WithServiceName("tracker-2"),
		metry.WithExporter(metrytest.MetrySpanExporter(memTrace2)),
		metry.WithMetricExporter(metrytest.MetryMetricExporter(memMetric2)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider2.Shutdown(context.Background()) })

	tracker1, err := NewTrackerFromProvider(provider1,
		WithRecordPayloads(true),
		WithMaxContextLength(96),
	)
	require.NoError(t, err)
	tracker2, err := NewTrackerFromProvider(provider2,
		WithRecordPayloads(false),
		WithMaxContextLength(32),
	)
	require.NoError(t, err)

	toolJSON := `{"prompt":"` + strings.Repeat("a", 512) + `"}`

	ctx1, end1 := tracker1.StartToolSpan(context.Background(), "search", "call-1", toolJSON)
	require.NoError(t, tracker1.RecordInteraction(ctx1, testMeta(), testPayload(), Usage{
		InputTokens:           12,
		OutputTokens:          6,
		CacheReadInputTokens:  3,
		ReasoningOutputTokens: 2,
	}))
	tracker1.RecordToolResult(ctx1, `{"result":"`+strings.Repeat("b", 512)+`"}`, nil)
	end1()

	ctx2, end2 := tracker2.StartToolSpan(context.Background(), "search", "call-2", toolJSON)
	require.NoError(t, tracker2.RecordInteraction(ctx2, testMeta(), testPayload(), Usage{
		InputTokens:  4,
		OutputTokens: 2,
	}))
	tracker2.RecordToolResult(ctx2, `{"result":"ok"}`, nil)
	end2()

	flushTestProvider(t, provider1)
	flushTestProvider(t, provider2)

	spans1 := memTrace1.GetSpans()
	require.Len(t, spans1, 2)
	chatSpan1 := testutil.SpanByName(t, spans1, "chat")
	require.True(t, testutil.SpanStubHasAttr(chatSpan1, InputMessages))

	spans2 := memTrace2.GetSpans()
	require.Len(t, spans2, 2)
	chatSpan2 := testutil.SpanByName(t, spans2, "chat")
	require.False(t, testutil.SpanStubHasAttr(chatSpan2, InputMessages))

	rm1 := metrytest.CollectResourceMetrics(t, provider1, memMetric1)
	assert.InDelta(
		t,
		12,
		metrytest.Int64HistogramSumByAttr(t, rm1, TokenUsageMetricName, TokenType, TokenTypeInput),
		1e-9,
	)
	assert.InDelta(
		t,
		6,
		metrytest.Int64HistogramSumByAttr(t, rm1, TokenUsageMetricName, TokenType, TokenTypeOutput),
		1e-9,
	)
	assert.InDelta(
		t,
		3,
		metrytest.Int64HistogramSumByAttr(t, rm1, TokenComponentUsageMetricName, TokenType, TokenTypeInputCacheRead),
		1e-9,
	)
	assert.InDelta(
		t,
		2,
		metrytest.Int64HistogramSumByAttr(t, rm1, TokenComponentUsageMetricName, TokenType, TokenTypeOutputReasoning),
		1e-9,
	)

	rm2 := metrytest.CollectResourceMetrics(t, provider2, memMetric2)
	assert.InDelta(
		t,
		4,
		metrytest.Int64HistogramSumByAttr(t, rm2, TokenUsageMetricName, TokenType, TokenTypeInput),
		1e-9,
	)
	assert.InDelta(
		t,
		2,
		metrytest.Int64HistogramSumByAttr(t, rm2, TokenUsageMetricName, TokenType, TokenTypeOutput),
		1e-9,
	)
	metrytest.AssertMetricAbsent(t, rm2, TokenComponentUsageMetricName)
}
