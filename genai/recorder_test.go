package genai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRuntime_RecordOperation_SetsAttributesAndMetrics(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t, WithRawPayloads())
	rec := tracker.Runtime()
	payload := Payload{
		InputMessages: []Message{{
			Role:  "user",
			Parts: []ContentPart{{Type: "text", Content: "extract fields"}},
		}},
	}

	err := rec.RecordOperation(
		context.Background(),
		Operation{Locality: ExecutionRemote, Name: "extract", Model: "model", Purpose: PurposeGeneration},
		OperationResult{
			Status:   OperationStatusOK,
			Duration: 2 * time.Second,
			Usage: Usage{
				InputTokens:  Observe(5, UnitTokens, SourceMeasured),
				OutputTokens: Observe(3, UnitTokens, SourceMeasured),
			},
			Payload: payload,
		},
		metry.IntAttribute("custom.empty_fields", 2),
	)

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "extract model", span.Name)
	assert.Equal(t, unknownProviderName, testutil.SpanStubStringAttr(t, span, ProviderName))
	assert.Equal(t, "extract", testutil.SpanStubStringAttr(t, span, OperationName))
	assert.Equal(t, "model", testutil.SpanStubStringAttr(t, span, RequestModel))
	assert.Equal(t, OperationStatusOK, testutil.SpanStubStringAttr(t, span, OperationStatus))
	assert.Equal(t, int64(2), testutil.SpanStubInt64Attr(t, span, "custom.empty_fields"))
	assert.Contains(t, testutil.SpanStubStringAttr(t, span, InputMessages), "extract fields")

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(
		t,
		2,
		metrytest.Float64HistogramSum(t, rm, metrytest.MetricSelector{Name: ObservedOperationDurationMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		5,
		testInt64HistogramSum(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		3,
		testInt64HistogramSum(t, rm, metrytest.MetricSelector{Name: OutputTokenUsageMetricName}),
		1e-9,
	)
}

func TestRuntime_RecordOperation_ErrorStatusSetsErrorType(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)
	rec := tracker.Runtime()

	err := rec.RecordOperation(
		context.Background(),
		Operation{Locality: ExecutionRemote, Name: "extract"},
		OperationResult{Status: OperationStatusError, Duration: time.Second},
	)

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, OperationStatusError, testutil.SpanStubStringAttr(t, spans[0], OperationStatus))
	assert.Equal(t, string(ErrorClassUnknown), testutil.SpanStubStringAttr(t, spans[0], ErrorType))
	testutil.AssertSpanStubErrorStatus(t, spans[0])
}

func TestRuntime_RecordOperation_WithoutPayloadPolicySkipsPayload(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)
	rec := tracker.Runtime()

	err := rec.RecordOperation(
		context.Background(),
		Operation{Locality: ExecutionRemote, Name: "extract"},
		OperationResult{
			Status: OperationStatusOK,
			Payload: Payload{
				InputMessages: []Message{{
					Role:  "user",
					Parts: []ContentPart{{Type: "text", Content: "secret"}},
				}},
			},
		},
	)

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, testutil.SpanStubHasAttr(spans[0], InputMessages))
}

func TestNoopRuntime_DropsOperationsWithoutPanic(t *testing.T) {
	rec := NoopRuntime()

	require.NoError(t, rec.RecordOperation(
		context.Background(),
		Operation{Locality: ExecutionRemote, Name: "noop"},
		OperationResult{Status: OperationStatusOK},
	))
	_, handle, err := rec.StartTool(context.Background(), ToolCall{Name: "search"})
	require.NoError(t, err)
	require.NoError(t, handle.Finish(ToolResult{Status: OperationStatusOK, Result: `{"ok":true}`}))
	require.ErrorIs(t, handle.Close(), ErrFinalized)
}
