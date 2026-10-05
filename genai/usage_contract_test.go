package genai

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestUsageValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		usage Usage
		rule  string
	}{
		{"absent", Usage{}, ""},
		{"zero", Usage{InputTokens: Observe(0, UnitTokens, SourceMeasured)}, ""},
		{"estimate", Usage{InputTokens: Observe(0, UnitTokens, SourceEstimated)}, ""},
		{"mixed", Usage{InputTokens: Observe(100, UnitTokens, SourceProvider), CacheReadInputTokens: Observe(60, UnitTokens, SourceProvider), CacheWriteInputTokens: Observe(60, UnitTokens, SourceEstimated)}, ""},
		{"negative", Usage{InputTokens: Observe(-1, UnitTokens, SourceMeasured)}, "invalid_value"},
		{"wrong_unit", Usage{InputTokens: Observe(1, UnitFrames, SourceMeasured)}, "incompatible_unit"},
		{"unknown_present", Usage{InputTokens: Observe(1, UnitTokens, SourceUnknown)}, "invalid_source"},
		{"absent_value", Usage{InputTokens: Measurement[int]{Value: 2}}, "contradictory_absence"},
		{"component", Usage{InputTokens: Observe(10, UnitTokens, SourceProvider), CacheReadInputTokens: Observe(11, UnitTokens, SourceProvider)}, "exceeds_total"},
		{"partial_component", Usage{CacheReadInputTokens: Observe(11, UnitTokens, SourceProvider)}, ""},
		{"currency_missing", Usage{Cost: Observe(0.1, UnitCurrency, SourceProvider)}, "required_currency_code"},
		{"mapping", Usage{Mapping: "secret-unregistered-mapping"}, "unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange.
			usage := test.usage
			// Act.
			err := usage.Validate()
			// Assert.
			if test.rule == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidObservation)
			var diagnostic *ObservationError
			require.ErrorAs(t, err, &diagnostic)
			assert.Equal(t, test.rule, diagnostic.Rule)
			assert.NotContains(t, err.Error(), "secret-unregistered")
		})
	}
}

func TestUsageZeroMissingEstimated(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	usages := []Usage{
		{},
		{InputTokens: Observe(0, UnitTokens, SourceMeasured)},
		{InputTokens: Observe(0, UnitTokens, SourceEstimated)},
	}
	// Act.
	for _, usage := range usages {
		require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, usage))
	}
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert.
	spans := traceExporter.GetSpans()
	require.Len(t, spans, 3)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], InputTokens))
	assert.True(t, testutil.SpanStubHasAttr(spans[1], InputTokens))
	assert.Equal(
		t,
		"measured",
		testutil.SpanStubStringAttr(t, spans[1], "metry.gen_ai.measurement.input_tokens.source"),
	)
	assert.Equal(
		t,
		"estimated",
		testutil.SpanStubStringAttr(t, spans[2], "metry.gen_ai.measurement.input_tokens.source"),
	)
	histogram := metrytest.FindInt64Histogram(t, metrics, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
	require.Len(t, histogram.DataPoints, 2)
	for _, point := range histogram.DataPoints {
		assert.Equal(t, uint64(1), point.Count)
		assert.Equal(t, int64(0), point.Sum)
	}
}

func TestNonFiniteUsageRejectedBeforeInstrumentation(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	// Act.
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		usage := Usage{Cost: Observe(value, UnitCurrency, SourceProvider), Currency: "USD"}
		require.ErrorIs(
			t,
			tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, usage),
			ErrInvalidObservation,
		)
		require.ErrorIs(
			t,
			tracker.Runtime().
				RecordOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: "chat"}, OperationResult{Usage: usage}),
			ErrInvalidObservation,
		)
		usage = Usage{VideoSeconds: Observe(value, UnitSeconds, SourceMeasured)}
		require.ErrorIs(
			t,
			tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, usage),
			ErrInvalidObservation,
		)
	}
	require.NoError(
		t,
		tracker.RecordInteraction(
			context.Background(),
			testMeta(),
			Payload{},
			Usage{Cost: Observe(0.25, UnitCurrency, SourceProvider), Currency: "USD"},
		),
	)
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: rejected inputs cannot poison later cumulative metrics.
	require.Len(t, traceExporter.GetSpans(), 1)
	assert.InDelta(t, 0.25, metrytest.Float64SumValue(t, metrics, metrytest.MetricSelector{Name: CostMetricName}), 1e-9)
}
