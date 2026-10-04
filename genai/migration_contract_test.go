package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestMigrationPreservesKnownZeroUnitsAndHostCustomNames(t *testing.T) {
	// Arrange: absence cannot be recovered from an old scalar zero. The host must
	// supply knowledge of presence and provenance at its own adapter boundary.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	input := struct {
		tokens int
		known  bool
		source Source
	}{tokens: 0, known: true, source: SourceMeasured}
	usage := Usage{Mapping: profileInclusiveMapping}
	if input.known {
		usage.InputTokens = Observe(input.tokens, UnitTokens, input.source)
	}
	// Act: logical and physical boundary choices are explicit, not inferred.
	require.NoError(t, tracker.Runtime().RecordOperation(context.Background(),
		Operation{Name: profileChatOperation}, OperationResult{Usage: usage},
		metry.StringAttribute("host.custom.name", "safe-static-value")))
	_, summary, err := tracker.Runtime().StartOperation(context.Background(),
		Operation{
			Name: "logical",
		}, RecordingOptions{Identity: CausalIdentity{RunID: "run", OperationID: "operation", Kind: IdentityLogical}})
	require.NoError(t, err)
	require.NoError(t, summary.Finish(OperationResult{Usage: usage}))
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: wire names/unit/provenance and known-zero count survive migration;
	// logical summary and subsets don't create another physical accounting event.
	spans := traceExporter.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, "safe-static-value", testutil.SpanStubStringAttr(t, spans[0], "host.custom.name"))
	assert.Equal(t, string(UnitTokens), testutil.SpanStubStringAttr(t, spans[0], InputTokens+".unit"))
	assert.Equal(t, string(SourceMeasured), testutil.SpanStubStringAttr(t, spans[0], InputTokens+".source"))
	assert.Equal(t, "completed", testutil.SpanStubStringAttr(t, spans[0], "metry.observation.kind"))
	histogram := metrytest.FindInt64Histogram(t, metrics, TokenUsageMetricName)
	require.Len(t, histogram.DataPoints, 1)
	assert.Equal(t, uint64(1), histogram.DataPoints[0].Count)
	assert.Equal(t, int64(0), histogram.DataPoints[0].Sum)
}
