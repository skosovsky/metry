package genai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRecordInteraction_RecordsTokenHistogramAndCost(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)

	require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, Usage{
		InputTokens:  10,
		OutputTokens: 20,
		Cost:         0.001,
	}))

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(
		t,
		10,
		metrytest.Int64HistogramSumByAttr(t, rm, TokenUsageMetricName, TokenType, TokenTypeInput),
		1e-9,
	)
	assert.InDelta(
		t,
		20,
		metrytest.Int64HistogramSumByAttr(t, rm, TokenUsageMetricName, TokenType, TokenTypeOutput),
		1e-9,
	)
	assert.InDelta(t, 0.001, metrytest.Float64SumValue(t, rm, CostMetricName), 1e-9)
	assert.Equal(t, "USD", metrytest.FirstFloat64SumAttr(t, rm, CostMetricName, CostCurrency))
}

func TestRecordInteraction_NegativeCost_IsIgnored(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t)

	require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, Usage{
		InputTokens:  10,
		OutputTokens: 20,
		Cost:         -0.25,
		Currency:     "USD",
	}))

	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], UsageCost))

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	metrytest.AssertMetricAbsent(t, rm, CostMetricName)
}

func TestRecordTTFT_AndStreamingCompletion_RecordMetrics(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)

	tracker.RecordTTFT(context.Background(), testMeta(), 420*time.Millisecond)
	tracker.RecordStreamingCompletion(context.Background(), testMeta(), 11, time.Second, 6*time.Second)

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(t, 0.42, metrytest.Float64HistogramSum(t, rm, TTFTMetricName), 1e-9)
	assert.InDelta(t, 2.2, metrytest.Float64HistogramSum(t, rm, StreamingTPSMetricName), 1e-9)
	assert.InDelta(t, 0.5, metrytest.Float64HistogramSum(t, rm, StreamingTBTMetricName), 1e-9)
}

func TestRecordTTFT_WithScope_UsesScopeProviderInMetrics(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)

	ctx := WithScope(context.Background(), Scope{Provider: "scope-provider", Operation: "chat"})
	tracker.RecordTTFT(ctx, Meta{}, 100*time.Millisecond)

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(t, rm, TTFTMetricName)
	require.NotEmpty(t, hist.DataPoints)
	assert.Equal(t, "scope-provider", testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ProviderName))
}

func TestRecordStreamingCompletion_WithScope_UsesScopeProviderInMetrics(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)

	ctx := WithScope(context.Background(), Scope{Provider: "scope-provider", Operation: "chat"})
	tracker.RecordStreamingCompletion(ctx, Meta{}, 10, time.Second, 5*time.Second)

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(t, rm, StreamingTPSMetricName)
	require.NotEmpty(t, hist.DataPoints)
	assert.Equal(t, "scope-provider", testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ProviderName))
}

func TestRecordInteraction_WithScopeModelOnly_SkipsTokenMetrics(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)

	ctx := WithScope(context.Background(), Scope{Model: "gpt-4o-mini"})
	require.NoError(t, tracker.RecordInteraction(ctx, Meta{}, Payload{}, Usage{
		InputTokens:  10,
		OutputTokens: 5,
	}))

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == TokenUsageMetricName {
				t.Fatalf("expected %q to be skipped when scope has Model only", TokenUsageMetricName)
			}
		}
	}
}
