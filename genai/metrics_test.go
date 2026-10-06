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
		InputTokens: Observe(10, UnitTokens, SourceMeasured),

		OutputTokens: Observe(20, UnitTokens, SourceMeasured),

		Cost:     Observe(float64(0.001), UnitCurrency, SourceMeasured),
		Currency: "USD",
	}))

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(
		t,
		10,
		testInt64HistogramSum(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		20,
		testInt64HistogramSum(t, rm, metrytest.MetricSelector{Name: OutputTokenUsageMetricName}),
		1e-9,
	)
	assert.InDelta(t, 0.001, metrytest.Float64SumValue(t, rm, metrytest.MetricSelector{Name: CostMetricName}), 1e-9)
	assert.Equal(
		t,
		"USD",
		metrytest.FirstFloat64SumAttr(t, rm, metrytest.MetricSelector{Name: CostMetricName}, CostCurrency),
	)
}

func TestRecordInteraction_NegativeCost_IsRejectedAtomically(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t)

	require.ErrorIs(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, Usage{
		InputTokens: Observe(10, UnitTokens, SourceMeasured),

		OutputTokens: Observe(20, UnitTokens, SourceMeasured),

		Cost: Observe(float64(-0.25), UnitCurrency, SourceMeasured),

		Currency: "USD",
	}), ErrInvalidObservation)

	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Empty(t, spans)

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: CostMetricName})
}

func TestRecordStreamingCompletion_RecordsMetrics(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)

	require.NoError(
		t,
		tracker.RecordStreamingCompletion(
			context.Background(),
			StreamingCompletion{
				Meta:              testMeta(),
				Usage:             Usage{OutputTokens: Observe(11, UnitTokens, SourceProvider)},
				TTFT:              time.Second,
				FirstTokenPresent: true,
				TotalDuration:     6 * time.Second,
			},
		),
	)

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(t, 1.0, metrytest.Float64HistogramSum(t, rm, metrytest.MetricSelector{Name: TTFTMetricName}), 1e-9)
	assert.InDelta(
		t,
		2.2,
		metrytest.Float64HistogramSum(t, rm, metrytest.MetricSelector{Name: DerivedOutputTokensPerSecondMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		0.5,
		metrytest.Float64HistogramSum(t, rm, metrytest.MetricSelector{Name: DerivedMeanOutputTokenIntervalMetricName}),
		1e-9,
	)
}

func TestRecordStreamingCompletion_WithScope_UsesScopeProviderInMetrics(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)

	ctx := WithScope(context.Background(), Scope{Provider: "scope-provider", Operation: "chat"})
	require.NoError(
		t,
		tracker.RecordStreamingCompletion(
			ctx,
			StreamingCompletion{
				Usage:             Usage{OutputTokens: Observe(10, UnitTokens, SourceProvider)},
				TTFT:              time.Second,
				FirstTokenPresent: true,
				TotalDuration:     5 * time.Second,
			},
		),
	)

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(
		t,
		rm,
		metrytest.MetricSelector{Name: DerivedOutputTokensPerSecondMetricName},
	)
	require.NotEmpty(t, hist.DataPoints)
	assert.Equal(t, "scope-provider", testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ProviderName))
}

func TestRecordInteraction_WithScopeModelOnly_RejectsMissingOperation(t *testing.T) {
	// Arrange.
	tracker, provider, memMetric, exporter := newTestTrackerWithMetrics(t)

	ctx := WithScope(context.Background(), Scope{Model: "gpt-4o-mini"})
	// Act.
	err := tracker.RecordInteraction(ctx, Meta{}, Payload{}, Usage{
		InputTokens: Observe(10, UnitTokens, SourceMeasured),

		OutputTokens: Observe(5, UnitTokens, SourceMeasured),
	})
	flushTestProvider(t, provider)

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	// Assert: no silent usage loss or malformed span under the new contract.
	require.ErrorIs(t, err, ErrInvalidObservation)
	assert.Empty(t, exporter.GetSpans())
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
}
