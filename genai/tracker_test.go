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
		metry.WithExporter(memTrace1.SDKSpanExporter()),
		metry.WithMetricExporter(memMetric1.SDKExporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider1.Shutdown(context.Background()) })

	memMetric2 := testutil.NewInMemoryMetricExporter()
	memTrace2 := testutil.NewInMemoryTraceExporter()
	provider2, err := metry.New(context.Background(),
		metry.WithServiceName("tracker-2"),
		metry.WithExporter(memTrace2.SDKSpanExporter()),
		metry.WithMetricExporter(memMetric2.SDKExporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider2.Shutdown(context.Background()) })

	tracker1, err := NewTrackerFromProvider(provider1,
		WithPayloadPolicy(RedactPayloadPolicy()),
		WithPayloadByteLimit(96),
	)
	require.NoError(t, err)
	tracker2, err := NewTrackerFromProvider(provider2,
		WithPayloadByteLimit(32),
	)
	require.NoError(t, err)

	toolJSON := `{"prompt":"` + strings.Repeat("a", 512) + `"}`
	recorder1 := tracker1.Recorder()
	recorder2 := tracker2.Recorder()

	ctx1, end1, startErr := recorder1.StartTool(context.Background(), ToolCall{
		Name:      "search",
		CallID:    "call-1",
		Arguments: toolJSON,
	})
	require.NoError(t, startErr)
	require.NoError(t, tracker1.RecordInteraction(ctx1, testMeta(), testPayload(), Usage{
		InputTokens: Observe(12, UnitTokens, SourceMeasured),

		OutputTokens: Observe(6, UnitTokens, SourceMeasured),

		CacheReadInputTokens: Observe(3, UnitTokens, SourceMeasured),

		ReasoningOutputTokens: Observe(2, UnitTokens, SourceMeasured),
	}))
	require.NoError(
		t,
		end1.Finish(ToolResult{Status: OperationStatusOK, Result: `{"result":"` + strings.Repeat("b", 512) + `"}`}),
	)

	ctx2, end2, startErr := recorder2.StartTool(context.Background(), ToolCall{
		Name:      "search",
		CallID:    "call-2",
		Arguments: toolJSON,
	})
	require.NoError(t, startErr)
	require.NoError(t, tracker2.RecordInteraction(ctx2, testMeta(), testPayload(), Usage{
		InputTokens: Observe(4, UnitTokens, SourceMeasured),

		OutputTokens: Observe(2, UnitTokens, SourceMeasured),
	}))
	require.NoError(t, end2.Finish(ToolResult{Status: OperationStatusOK, Result: `{"result":"ok"}`}))

	flushTestProvider(t, provider1)
	flushTestProvider(t, provider2)

	spans1 := memTrace1.GetSpans()
	require.Len(t, spans1, 2)
	chatSpan1 := testutil.SpanByName(t, spans1, "chat "+fingerprintPayloadLabel("gpt-4o-mini"))
	require.True(t, testutil.SpanStubHasAttr(chatSpan1, InputMessages))

	spans2 := memTrace2.GetSpans()
	require.Len(t, spans2, 2)
	chatSpan2 := testutil.SpanByName(t, spans2, "chat "+fingerprintPayloadLabel("gpt-4o-mini"))
	require.False(t, testutil.SpanStubHasAttr(chatSpan2, InputMessages))

	rm1 := metrytest.CollectResourceMetrics(t, provider1, memMetric1)
	assert.InDelta(
		t,
		12,
		testInt64HistogramSum(t, rm1, metrytest.MetricSelector{Name: InputTokenUsageMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		6,
		testInt64HistogramSum(t, rm1, metrytest.MetricSelector{Name: OutputTokenUsageMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		3,
		metrytest.Int64HistogramSumByAttr(
			t,
			rm1,
			metrytest.MetricSelector{Name: TokenComponentUsageMetricName},
			TokenType,
			TokenComponentCacheRead,
		),
		1e-9,
	)
	assert.InDelta(
		t,
		2,
		metrytest.Int64HistogramSumByAttr(
			t,
			rm1,
			metrytest.MetricSelector{Name: TokenComponentUsageMetricName},
			TokenType,
			TokenComponentReasoning,
		),
		1e-9,
	)

	rm2 := metrytest.CollectResourceMetrics(t, provider2, memMetric2)
	assert.InDelta(
		t,
		4,
		testInt64HistogramSum(t, rm2, metrytest.MetricSelector{Name: InputTokenUsageMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		2,
		testInt64HistogramSum(t, rm2, metrytest.MetricSelector{Name: OutputTokenUsageMetricName}),
		1e-9,
	)
	metrytest.AssertMetricAbsent(t, rm2, metrytest.MetricSelector{Name: TokenComponentUsageMetricName})
}
