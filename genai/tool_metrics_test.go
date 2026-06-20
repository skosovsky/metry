package genai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRecorder_ToolLifecycle_RecordsDurationMetricOnce(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t, WithRawPayloads())
	rec := tracker.Recorder()

	ctx, end := rec.StartTool(context.Background(), ToolCall{Name: "search", CallID: "call-1", Arguments: `{"q":"x"}`})
	rec.RecordToolResult(ctx, ToolResult{Result: `{"ok":true}`})
	end()
	end()

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(t, rm, ToolDurationMetricName)
	require.Len(t, hist.DataPoints, 1)
	assert.Greater(t, hist.DataPoints[0].Sum, 0.0)
	assert.Equal(t, "search", testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelTool))
	assert.Equal(t, OperationStatusOK, testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelStatus))
}

func TestRecorder_ToolLifecycle_RecordsErrorMetricWithBoundedLabels(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t)
	rec := tracker.Recorder()

	ctx, end := rec.StartTool(context.Background(), ToolCall{Name: "", CallID: "call-1"})
	rec.RecordToolResult(ctx, ToolResult{
		Err:       errors.New("contains user email bob@example.com"),
		ErrorType: "timeout with user data and spaces",
	})
	end()

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(t, rm, ToolDurationMetricName)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(t, unknownToolName, testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelTool))
	assert.Equal(
		t,
		OperationStatusError,
		testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelStatus),
	)
	assert.Equal(
		t,
		OperationStatusError,
		testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelErrorType),
	)

	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Len(t, spans, 1)
	assert.NotContains(t, spans[0].Status.Description, "bob@example.com")
	for _, event := range spans[0].Events {
		for _, attr := range event.Attributes {
			assert.NotContains(t, attr.Value.AsString(), "bob@example.com")
		}
	}
}
