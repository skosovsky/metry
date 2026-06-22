package genai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRuntime_ForProvider_RecordOperationBindsProvider(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t)
	runtime := tracker.Runtime().ForProvider("gemini")

	err := runtime.RecordOperation(
		context.Background(),
		Operation{Name: "expand_query", Model: "model"},
		OperationResult{
			Status:   OperationStatusOK,
			Duration: 2 * time.Second,
			Usage:    Usage{InputTokens: 3},
		},
	)

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "gemini", testutil.SpanStubStringAttr(t, spans[0], ProviderName))

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(t, 2, metrytest.Float64HistogramSum(t, rm, OperationDurationMetricName), 1e-9)
	assert.InDelta(
		t,
		3,
		metrytest.Int64HistogramSumByAttr(t, rm, TokenUsageMetricName, ProviderName, "gemini"),
		1e-9,
	)
}

func TestRuntime_RecordOperation_DefaultsProviderWhenUnbound(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t)
	runtime := tracker.Runtime()

	err := runtime.RecordOperation(
		context.Background(),
		Operation{Name: "expand_query", Model: "model"},
		OperationResult{
			Status:   OperationStatusOK,
			Duration: time.Second,
			Usage:    Usage{InputTokens: 2},
		},
	)

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, unknownProviderName, testutil.SpanStubStringAttr(t, spans[0], ProviderName))

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(
		t,
		2,
		metrytest.Int64HistogramSumByAttr(t, rm, TokenUsageMetricName, ProviderName, unknownProviderName),
		1e-9,
	)
}

func TestRuntime_RecordTraceIOAndStreamingMetricsUseRuntimeBoundary(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t, WithRawPayloads())
	runtime := tracker.Runtime().ForProvider("gemini")
	ctx := WithScope(context.Background(), Scope{Operation: "chat"})

	spanCtx, end, err := provider.StartSpan(ctx, "handler", "chat-handler")
	require.NoError(t, err)
	err = runtime.RecordTraceIO(
		spanCtx,
		Payload{
			SystemInstructions: nil,
			InputMessages: []Message{{
				Role:         "user",
				Parts:        []ContentPart{{Type: "text", Content: "question"}},
				FinishReason: "",
			}},
			OutputMessages: nil,
		},
		Payload{
			SystemInstructions: nil,
			InputMessages:      nil,
			OutputMessages: []Message{{
				Role:         "assistant",
				Parts:        []ContentPart{{Type: "text", Content: "answer"}},
				FinishReason: "",
			}},
		},
	)
	require.NoError(t, err)
	runtime.RecordTTFT(ctx, 200*time.Millisecond)
	runtime.RecordStreamingCompletion(ctx, StreamingCompletion{
		Meta:          Meta{Provider: "caller-provider"},
		OutputTokens:  5,
		TTFT:          time.Second,
		TotalDuration: 3 * time.Second,
	})
	end()

	flushTestProvider(t, provider)
	spans := memTrace.GetSpans()
	require.Len(t, spans, 1)
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], InputMessages), "question")
	assert.Contains(t, testutil.SpanStubStringAttr(t, spans[0], OutputMessages), "answer")
	assert.Equal(t, "gemini", testutil.SpanStubStringAttr(t, spans[0], ProviderName))

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	assert.InDelta(t, 0.2, metrytest.Float64HistogramSum(t, rm, TTFTMetricName), 1e-9)
	assert.InDelta(t, 2.5, metrytest.Float64HistogramSum(t, rm, StreamingTPSMetricName), 1e-9)
	assert.InDelta(t, 0.5, metrytest.Float64HistogramSum(t, rm, StreamingTBTMetricName), 1e-9)
}

func TestRuntime_AsyncTokenResult_SuccessFiltersReservedCallerAttributes(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)
	runtime := tracker.Runtime().ForProvider("gemini")

	handle, err := runtime.StartAsync(context.Background(), "enqueue_summary")
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)

	err = runtime.RecordAsyncTokenResult(context.Background(), token, AsyncResult{
		Name:   "summary_done",
		Status: OperationStatusOK,
		Attributes: []metry.Attribute{
			metry.StringAttribute(ProviderName, "caller-provider"),
			metry.StringAttribute(OperationStatus, OperationStatusError),
			metry.StringAttribute(ErrorType, "caller_error"),
			metry.StringAttribute("job_id", "job-1"),
		},
	})

	require.NoError(t, err)
	flushTestProvider(t, provider)
	outcome := testutil.SpanByName(t, mem.GetSpans(), "summary_done")
	assert.Equal(t, "gemini", testutil.SpanStubStringAttr(t, outcome, ProviderName))
	assert.Equal(t, OperationStatusOK, testutil.SpanStubStringAttr(t, outcome, OperationStatus))
	assert.False(t, testutil.SpanStubHasAttr(outcome, ErrorType))
	assert.Equal(t, "job-1", testutil.SpanStubStringAttr(t, outcome, "job_id"))
	testutil.AssertSpanStubOkStatus(t, outcome)
}

func TestRuntime_AsyncTokenResult_RecordsLinkedOutcomeWithoutContextTunnel(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)
	runtime := tracker.Runtime().ForProvider("gemini")

	handle, err := runtime.StartAsync(
		context.Background(),
		"enqueue_summary",
		WithAsyncAttributes(metry.StringAttribute("queue", "summaries")),
	)
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)

	err = runtime.RecordAsyncTokenResult(context.Background(), token, AsyncResult{
		Name:       "summary_done",
		Status:     OperationStatusError,
		ErrorClass: ErrorClassPaused,
		Attributes: []metry.Attribute{
			metry.StringAttribute(ProviderName, "caller-provider"),
			metry.StringAttribute(OperationStatus, OperationStatusOK),
			metry.StringAttribute(ErrorType, "caller_error"),
			metry.StringAttribute("job_id", "job-1"),
		},
	})

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	origin := testutil.SpanByName(t, spans, "enqueue_summary")
	outcome := testutil.SpanByName(t, spans, "summary_done")
	testutil.AssertLinkBasedAsyncSpan(t, outcome, origin)
	assert.Equal(t, "summaries", testutil.SpanStubStringAttr(t, origin, "queue"))
	assert.Equal(t, "gemini", testutil.SpanStubStringAttr(t, outcome, ProviderName))
	assert.Equal(t, OperationStatusError, testutil.SpanStubStringAttr(t, outcome, OperationStatus))
	assert.Equal(t, string(ErrorClassPaused), testutil.SpanStubStringAttr(t, outcome, ErrorType))
	assert.Equal(t, "job-1", testutil.SpanStubStringAttr(t, outcome, "job_id"))
	testutil.AssertSpanStubErrorStatus(t, outcome)
}

func TestRuntime_AsyncTokenResult_StatusOnlyErrorDefaultsUnknown(t *testing.T) {
	tracker, provider, mem := newTestTracker(t)
	runtime := tracker.Runtime()

	handle, originSC := metrytest.AsyncHandleFromProducerSpan(t, provider, mem, "producer", "enqueue")
	token, err := handle.Marshal()
	require.NoError(t, err)

	err = runtime.RecordAsyncTokenResult(context.Background(), token, AsyncResult{
		Name:   "summary_done",
		Status: OperationStatusError,
	})

	require.NoError(t, err)
	flushTestProvider(t, provider)
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	outcome := testutil.SpanByName(t, spans, "summary_done")
	testutil.AssertLinkBasedAsyncSpan(t, outcome, originSC)
	assert.Equal(t, OperationStatusError, testutil.SpanStubStringAttr(t, outcome, OperationStatus))
	assert.Equal(t, string(ErrorClassUnknown), testutil.SpanStubStringAttr(t, outcome, ErrorType))
	testutil.AssertSpanStubErrorStatus(t, outcome)
}

func TestRuntime_AsyncTokenResult_ParseFailureReturnsError(t *testing.T) {
	tracker, _, _ := newTestTracker(t)
	runtime := tracker.Runtime()

	err := runtime.RecordAsyncTokenResult(context.Background(), "not-a-token", AsyncResult{Name: "done"})

	require.ErrorIs(t, err, metry.ErrInvalidAsyncHandle)
}

func TestNoopRuntime_StartAsyncReturnsSerializableHandle(t *testing.T) {
	runtime := NoopRuntime()

	handle, err := runtime.StartAsync(context.Background(), "enqueue")

	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	require.NoError(t, runtime.RecordAsyncTokenResult(context.Background(), token, AsyncResult{Name: "done"}))
	require.ErrorIs(
		t,
		runtime.RecordAsyncResult(context.Background(), metry.AsyncHandle{}, AsyncResult{Name: "done"}),
		ErrInvalidAsyncHandle,
	)
	require.ErrorIs(
		t,
		runtime.RecordAsyncTokenResult(context.Background(), "not-a-token", AsyncResult{Name: "done"}),
		metry.ErrInvalidAsyncHandle,
	)
}

func TestRuntime_ToolErrorClassifier_DefaultAndCustomClassesAreBounded(t *testing.T) {
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t)
	runtime := tracker.Runtime()

	ctx, end := runtime.StartTool(context.Background(), ToolCall{Name: "search"})
	runtime.RecordToolResult(ctx, ToolResult{Err: errors.Join(context.DeadlineExceeded)})
	end()

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(t, rm, ToolDurationMetricName)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(
		t,
		string(ErrorClassDeadlineExceeded),
		testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelErrorType),
	)
}

func TestRuntime_StartTool_ForProviderSetsProviderAttribute(t *testing.T) {
	tracker, provider, _, memTrace := newTestTrackerWithMetrics(t)
	runtime := tracker.Runtime().ForProvider("gemini")

	ctx, end := runtime.StartTool(
		context.Background(),
		ToolCall{Name: "search"},
		WithSpanAttributes(metry.StringAttribute(ProviderName, "caller-provider")),
	)
	runtime.RecordToolResult(ctx, ToolResult{Result: `{"ok":true}`})
	end()

	flushTestProvider(t, provider)
	span := testutil.SpanByName(t, memTrace.GetSpans(), "tool: search")
	assert.Equal(t, "gemini", testutil.SpanStubStringAttr(t, span, ProviderName))
}

func TestRuntime_ToolErrorClassifier_StatusOnlyErrorDefaultsUnknown(t *testing.T) {
	tracker, provider, memMetric, memTrace := newTestTrackerWithMetrics(t)
	runtime := tracker.Runtime()

	ctx, end := runtime.StartTool(context.Background(), ToolCall{Name: "search"})
	runtime.RecordToolResult(ctx, ToolResult{Status: OperationStatusError})
	end()

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(t, rm, ToolDurationMetricName)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(
		t,
		string(ErrorClassUnknown),
		testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelErrorType),
	)

	flushTestProvider(t, provider)
	span := testutil.SpanByName(t, memTrace.GetSpans(), "tool: search")
	assert.Equal(t, string(ErrorClassUnknown), testutil.SpanStubStringAttr(t, span, ErrorType))
	testutil.AssertSpanStubErrorStatus(t, span)
}

func TestRuntime_ToolErrorClassifier_CustomClassRequiresAllowList(t *testing.T) {
	classifier := ToolErrorClassifierFunc(func(error) ErrorClass {
		return ErrorClass("provider_overloaded")
	})
	tracker, provider, memMetric, _ := newTestTrackerWithMetrics(t, WithToolErrorClassifier(classifier))
	runtime := tracker.Runtime()

	ctx, end := runtime.StartTool(context.Background(), ToolCall{Name: "search"})
	runtime.RecordToolResult(ctx, ToolResult{Err: errors.New("429")})
	end()

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist := metrytest.FindFloat64Histogram(t, rm, ToolDurationMetricName)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(
		t,
		string(ErrorClassUnknown),
		testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelErrorType),
	)

	tracker, provider, memMetric, _ = newTestTrackerWithMetrics(t,
		WithToolErrorClassifier(classifier),
		WithAllowedToolErrorClasses("provider_overloaded"),
	)
	runtime = tracker.Runtime()
	ctx, end = runtime.StartTool(context.Background(), ToolCall{Name: "search"})
	runtime.RecordToolResult(ctx, ToolResult{Err: errors.New("429")})
	end()

	rm = metrytest.CollectResourceMetrics(t, provider, memMetric)
	hist = metrytest.FindFloat64Histogram(t, rm, ToolDurationMetricName)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(
		t,
		"provider_overloaded",
		testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelErrorType),
	)
}
