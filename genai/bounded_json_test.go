package genai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestBoundedJSONValidEnvelopeAtEveryBudget(t *testing.T) {
	for _, value := range []any{
		[]string{"small", strings.Repeat("世", 200), "last"},
		map[string]any{"a": "small", "b": strings.Repeat("secret", 200)},
		strings.Repeat("large", 200),
	} {
		for _, budget := range []int{len(truncatedJSONMarker), 30, 64, 128} {
			// Arrange: each input exceeds the declared budget.
			// Act.
			encoded, err := boundedJSON(value, budget)
			// Assert: no string-cut JSON, including minimum envelope budget.
			require.NoError(t, err)
			assert.LessOrEqual(t, len(encoded), budget)
			assert.True(t, json.Valid([]byte(encoded)))
			assert.Contains(t, encoded, `"truncated":true`)
			again, err := boundedJSON(value, budget)
			require.NoError(t, err)
			assert.Equal(t, encoded, again)
		}
	}
}

func TestBoundedJSONMinimumBudgetAndConstructorDiagnostic(t *testing.T) {
	// Arrange.
	_, provider, _ := newTestTracker(t)
	// Act and Assert.
	for _, budget := range []int{-1, 0, len(truncatedJSONMarker) - 1} {
		encoded, err := boundedJSON([]string{"secret"}, budget)
		require.ErrorIs(t, err, ErrInvalidObservation)
		assert.Empty(t, encoded)
		tracker, err := NewTrackerFromProvider(provider, WithMaxContextLength(budget))
		require.ErrorIs(t, err, ErrInvalidObservation)
		assert.Nil(t, tracker)
		assert.NotContains(t, err.Error(), "secret")
	}
}

func TestPayloadInputBoundsRejectBeforeSanitizerAndInstrumentation(t *testing.T) {
	for _, payload := range []Payload{
		{SystemInstructions: make([]ContentPart, maxPayloadEntries+1)},
		{SystemInstructions: []ContentPart{{Content: strings.Repeat("x", maxPayloadInputBytes+1)}}},
		{SystemInstructions: []ContentPart{{Arguments: json.RawMessage(strings.Repeat("[", maxPayloadJSONDepth+1) + "0" + strings.Repeat("]", maxPayloadJSONDepth+1))}}},
	} {
		// Arrange: a counting custom sanitizer proves validation precedes policy work.
		policy := &countingPayloadPolicy{calls: 0}
		tracker, provider, metricExporter, exporter := newTestTrackerWithMetrics(t, WithPayloadPolicy(policy))
		// Act.
		err := tracker.RecordInteraction(context.Background(), testMeta(), payload,
			Usage{InputTokens: Observe(10, UnitTokens, SourceProvider)})
		flushTestProvider(t, provider)
		metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
		// Assert.
		require.ErrorIs(t, err, ErrInvalidObservation)
		assert.Zero(t, policy.calls)
		assert.Empty(t, exporter.GetSpans())
		metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	}
}

type countingPayloadPolicy struct{ calls int }

func (p *countingPayloadPolicy) SanitizePayload(payload Payload) Payload {
	p.calls++
	return payload
}
func (*countingPayloadPolicy) SanitizeToolCall(call ToolCall) ToolCall         { return call }
func (*countingPayloadPolicy) SanitizeToolResult(result ToolResult) ToolResult { return result }

func TestInvalidPayloadCanBeCorrectedBeforeHandleFinalization(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads(), WithMaxContextLength(64))
	_, handle, err := tracker.Runtime().StartOperation(context.Background(),
		Operation{Name: profileChatOperation, Locality: ExecutionRemote}, RecordingOptions{})
	require.NoError(t, err)
	// Act.
	err = handle.Finish(
		OperationResult{Payload: Payload{SystemInstructions: []ContentPart{{Arguments: json.RawMessage("{")}}}},
	)
	require.ErrorIs(t, err, ErrInvalidObservation)
	require.NoError(
		t,
		handle.Finish(
			OperationResult{Payload: Payload{SystemInstructions: []ContentPart{{Content: strings.Repeat("世", 200)}}}},
		),
	)
	flushTestProvider(t, provider)
	// Assert: invalid finalize did not consume the local guard or export fragments.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	encoded := testutil.SpanStubStringAttr(t, spans[0], SystemInstructions)
	assert.True(t, json.Valid([]byte(encoded)))
	assert.LessOrEqual(t, len(encoded), 64)
	assert.Equal(t, "[]", encoded)
}

func TestToolStructuredPayloadLimitsAndBoundedDiagnostic(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads(), WithMaxContextLength(64))
	_, end, startErr := tracker.Runtime().StartTool(context.Background(), ToolCall{
		Name: "search", Arguments: `{"large":"` + strings.Repeat("secret", 200) + `"}`,
	})
	require.NoError(t, startErr)
	// Act.
	require.NoError(
		t,
		end.Finish(ToolResult{Status: OperationStatusOK, Result: strings.Repeat("secret", maxPayloadInputBytes)}),
	)
	flushTestProvider(t, provider)
	// Assert: only bounded valid JSON escapes; diagnostic contains field/rule, no raw.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	encoded := testutil.SpanStubStringAttr(t, spans[0], ToolCallArguments)
	assert.True(t, json.Valid([]byte(encoded)))
	assert.LessOrEqual(t, len(encoded), 64)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], ToolCallResult))
	require.Len(t, spans[0].Events, 1)
	assert.Equal(t, "metry.observation.rejected", spans[0].Events[0].Name)
	for _, attr := range spans[0].Events[0].Attributes {
		assert.NotContains(t, attr.Value.AsString(), "secret")
	}
}
