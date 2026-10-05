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

func TestInclusiveTotalsDoNotSumOverlappingAxes(t *testing.T) {
	// Arrange: subsets may overlap, so their sum may exceed the inclusive total.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	usage := Usage{
		InputTokens:              Observe(120, UnitTokens, SourceProvider),
		OutputTokens:             Observe(50, UnitTokens, SourceProvider),
		CacheReadInputTokens:     Observe(80, UnitTokens, SourceProvider),
		CacheCreationInputTokens: Observe(80, UnitTokens, SourceProvider),
		ReasoningOutputTokens:    Observe(40, UnitTokens, SourceEstimated),
		Mapping:                  profileInclusiveMapping,
	}
	// Act.
	require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, usage))
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: inclusive input remains 120, output remains 50, not 280/90.
	assert.InDelta(
		t,
		120,
		metrytest.Int64HistogramSumByAttr(
			t,
			metrics,
			metrytest.MetricSelector{Name: TokenUsageMetricName},
			TokenType,
			TokenTypeInput,
		),
		1e-9,
	)
	assert.InDelta(
		t,
		50,
		metrytest.Int64HistogramSumByAttr(
			t,
			metrics,
			metrytest.MetricSelector{Name: TokenUsageMetricName},
			TokenType,
			TokenTypeOutput,
		),
		1e-9,
	)
	require.Len(t, traceExporter.GetSpans(), 1)
	assert.True(t, testutil.SpanStubHasAttr(traceExporter.GetSpans()[0], CacheReadInputTokens))
	assert.True(t, testutil.SpanStubHasAttr(traceExporter.GetSpans()[0], CacheCreationInputTokens))
}

// exclusiveFixture is a synthetic caller adapter, not a backend mapping in core.
// The declaration promises these three buckets are exhaustive and disjoint.
type exclusiveFixture struct {
	mapping string
	fresh   Measurement[int]
	read    Measurement[int]
	created Measurement[int]
}

func (f exclusiveFixture) inclusive() (Usage, error) {
	if f.mapping != "exclusive.fixture" {
		return Usage{}, invalidObservation("mapping", "unsupported")
	}
	usage := Usage{
		CacheReadInputTokens:     f.read,
		CacheCreationInputTokens: f.created,
		Mapping:                  profileInclusiveMapping,
	}
	if err := usage.Validate(); err != nil {
		return Usage{}, err
	}
	if err := validateMeasurement("fresh", f.fresh, UnitTokens); err != nil {
		return Usage{}, err
	}
	if f.fresh.Present && f.read.Present && f.created.Present {
		if f.fresh.Source != f.read.Source || f.read.Source != f.created.Source {
			return Usage{}, invalidObservation("mapping", "mixed_source")
		}
		if f.fresh.Value > math.MaxInt-f.read.Value || f.fresh.Value+f.read.Value > math.MaxInt-f.created.Value {
			return Usage{}, invalidObservation("mapping", "overflow")
		}
		usage.InputTokens = Observe(f.fresh.Value+f.read.Value+f.created.Value, UnitTokens, f.fresh.Source)
	}
	return usage, usage.Validate()
}

func TestExclusiveCallerMappingRunsOnceAndPartialTotalStaysAbsent(t *testing.T) {
	// Arrange: complete input totals 120; the second breakdown is incomplete.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	complete := exclusiveFixture{
		mapping: "exclusive.fixture",
		fresh:   Observe(70, UnitTokens, SourceProvider),
		read:    Observe(30, UnitTokens, SourceProvider),
		created: Observe(20, UnitTokens, SourceProvider),
	}
	partial := complete
	partial.fresh = Measurement[int]{}
	// Act: adapters normalize once, metry consumes only the declared inclusive DTO.
	for _, fixture := range []exclusiveFixture{complete, partial} {
		usage, err := fixture.inclusive()
		require.NoError(t, err)
		require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{}, usage))
	}
	remapped := complete
	remapped.mapping = profileInclusiveMapping
	_, remapErr := remapped.inclusive()
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: components aren't added again; partial buckets don't manufacture total.
	require.ErrorIs(t, remapErr, ErrInvalidObservation)
	assert.InDelta(
		t,
		120,
		metrytest.Int64HistogramSumByAttr(
			t,
			metrics,
			metrytest.MetricSelector{Name: TokenUsageMetricName},
			TokenType,
			TokenTypeInput,
		),
		1e-9,
	)
	spans := traceExporter.GetSpans()
	require.Len(t, spans, 2)
	assert.True(t, testutil.SpanStubHasAttr(spans[0], InputTokens))
	assert.False(t, testutil.SpanStubHasAttr(spans[1], InputTokens))
	assert.True(t, testutil.SpanStubHasAttr(spans[1], CacheReadInputTokens))
}
