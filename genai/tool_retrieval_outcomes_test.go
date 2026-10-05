package genai

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestToolAndRetrievalOutcomeMatrix(t *testing.T) {
	cases := []struct {
		name                 string
		status               Outcome
		err                  error
		class                ErrorClass
		code                 codes.Code
		resolved, classLabel string
	}{
		{"success", OperationStatusOK, nil, "", codes.Ok, OperationStatusOK, ""},
		{"unknown", OperationStatusUnset, nil, "", codes.Unset, OperationStatusUnset, ""},
		{"empty", "", nil, "", codes.Unset, OperationStatusUnset, ""},
		{"error", OperationStatusError, nil, "", codes.Error, OperationStatusError, "unknown"},
		{"cancel", "", context.Canceled, "", codes.Error, OperationStatusError, "cancelled"},
		{"deadline", "", context.DeadlineExceeded, "", codes.Error, OperationStatusError, "deadline_exceeded"},
		{
			"abort",
			OperationStatusError,
			nil,
			ErrorClassStreamAborted,
			codes.Error,
			OperationStatusError,
			"stream_aborted",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
			_, tool, err := tracker.Runtime().StartTool(context.Background(), ToolCall{Name: "lookup"})
			require.NoError(t, err)
			_, retrieval, err := tracker.StartRetrieval(context.Background(), "lookup", RetrievalRequest{})
			require.NoError(t, err)
			// Act.
			require.NoError(t, tool.Finish(ToolResult{Status: tc.status, Err: tc.err, ErrorClass: tc.class}))
			require.NoError(t, retrieval.Finish(RetrievalResult{Status: tc.status, Err: tc.err, ErrorClass: tc.class}))
			flushTestProvider(t, provider)
			// Assert.
			spans := traceExporter.GetSpans()
			require.Len(t, spans, 2)
			for _, span := range spans {
				assert.Equal(t, tc.code, span.Status.Code)
				assert.Equal(t, tc.classLabel, span.Status.Description)
			}
			hist := metrytest.FindFloat64Histogram(
				t,
				metrytest.CollectResourceMetrics(t, provider, metricExporter),
				metrytest.MetricSelector{Name: ToolDurationMetricName},
			)
			require.Len(t, hist.DataPoints, 1)
			assert.Equal(
				t,
				tc.resolved,
				testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelStatus),
			)
			if tc.classLabel != "" {
				assert.Equal(
					t,
					tc.classLabel,
					testutil.SpanStringAttr(t, hist.DataPoints[0].Attributes, ToolMetricLabelErrorType),
				)
			}
		})
	}
}

func TestToolAndRetrievalInvalidFinishCorrectable(t *testing.T) {
	for _, recording := range []bool{false, true} {
		t.Run(map[bool]string{false: "noop", true: "recording"}[recording], func(t *testing.T) {
			// Arrange.
			var tracker *Tracker
			if recording {
				tracker, _, _ = newTestTracker(t)
			}
			_, tool, err := startTool(context.Background(), tracker, ToolCall{Name: "lookup"})
			require.NoError(t, err)
			_, retrieval, err := startRetrieval(context.Background(), tracker, "lookup", RetrievalRequest{})
			require.NoError(t, err)
			// Act and assert.
			for _, tc := range []ToolResult{{Status: "not-known"}, {Status: OperationStatusOK, Err: errors.New("private")}, {Status: OperationStatusUnset, ErrorClass: ErrorClassCancelled}, {ErrorClass: "raw personal message"}} {
				require.ErrorIs(t, tool.Finish(tc), ErrInvalidObservation)
				require.ErrorIs(
					t,
					retrieval.Finish(RetrievalResult{Status: tc.Status, Err: tc.Err, ErrorClass: tc.ErrorClass}),
					ErrInvalidObservation,
				)
			}
			require.NoError(t, tool.Finish(ToolResult{Status: OperationStatusOK}))
			require.NoError(t, retrieval.Finish(RetrievalResult{Status: OperationStatusOK}))
			require.ErrorIs(t, tool.Close(), ErrFinalized)
			require.ErrorIs(t, retrieval.Close(), ErrFinalized)
		})
	}
}

func TestToolAndRetrievalConcurrentFinishClose(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, exporter := newTestTrackerWithMetrics(t)
	_, tool, err := tracker.Runtime().StartTool(context.Background(), ToolCall{Name: "lookup"})
	require.NoError(t, err)
	_, retrieval, err := tracker.StartRetrieval(context.Background(), "lookup", RetrievalRequest{})
	require.NoError(t, err)
	var toolCommitted, retrievalCommitted, unexpectedErrors atomic.Int32
	var wait sync.WaitGroup
	// Act.
	for i := range 32 {
		wait.Go(func() {
			var toolErr, retrievalErr error
			if i%2 == 0 {
				toolErr = tool.Close()
				retrievalErr = retrieval.Close()
			} else {
				toolErr = tool.Finish(ToolResult{Status: OperationStatusOK})
				retrievalErr = retrieval.Finish(RetrievalResult{Status: OperationStatusOK})
			}
			if toolErr == nil {
				toolCommitted.Add(1)
			} else if !errors.Is(toolErr, ErrFinalized) {
				unexpectedErrors.Add(1)
			}
			if retrievalErr == nil {
				retrievalCommitted.Add(1)
			} else if !errors.Is(retrievalErr, ErrFinalized) {
				unexpectedErrors.Add(1)
			}
		})
	}
	wait.Wait()
	flushTestProvider(t, provider)
	// Assert.
	assert.Zero(t, unexpectedErrors.Load())
	assert.Equal(t, int32(1), toolCommitted.Load())
	assert.Equal(t, int32(1), retrievalCommitted.Load())
	require.Len(t, exporter.GetSpans(), 2)
	hist := metrytest.FindFloat64Histogram(
		t,
		metrytest.CollectResourceMetrics(t, provider, metrics),
		metrytest.MetricSelector{Name: ToolDurationMetricName},
	)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(t, uint64(1), hist.DataPoints[0].Count)
}

func TestRetrievalCopiesResultAndCloseHasNoChunks(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	distances := []float64{0.2, 0.3}
	_, resultHandle, err := tracker.StartRetrieval(context.Background(), "result", RetrievalRequest{})
	require.NoError(t, err)
	_, closeHandle, err := tracker.StartRetrieval(context.Background(), "close", RetrievalRequest{})
	require.NoError(t, err)
	// Act.
	require.NoError(
		t,
		resultHandle.Finish(RetrievalResult{Status: OperationStatusOK, ReturnedChunks: 2, Distances: distances}),
	)
	distances[0] = 99
	require.NoError(t, closeHandle.Close())
	flushTestProvider(t, provider)
	// Assert.
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	result, closed := spans[0], spans[1]
	assert.Equal(t, []float64{0.2, 0.3}, testutil.SpanStubFloat64SliceAttr(t, result, RetrievalDistances))
	assert.Equal(t, codes.Unset, closed.Status.Code)
	assert.False(t, testutil.SpanStubHasAttr(closed, RetrievalReturnedChunks))
}

func TestToolAndRetrievalZeroHandlesReject(t *testing.T) {
	// Arrange.
	tool := new(ToolHandle)
	retrieval := new(RetrievalHandle)
	// Act and assert.
	require.ErrorIs(t, tool.Finish(ToolResult{}), ErrInvalidObservation)
	require.ErrorIs(t, tool.Close(), ErrInvalidObservation)
	require.ErrorIs(t, retrieval.Finish(RetrievalResult{}), ErrInvalidObservation)
	require.ErrorIs(t, retrieval.Close(), ErrInvalidObservation)
}

func TestToolAndRetrievalDroppedSamplingStillValidate(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTrackerWithSampler(t, NewHintSampler(metry.NeverSample()))
	_, tool, err := tracker.Runtime().StartTool(context.Background(), ToolCall{Name: "lookup"})
	require.NoError(t, err)
	_, retrieval, err := tracker.StartRetrieval(context.Background(), "lookup", RetrievalRequest{})
	require.NoError(t, err)
	// Act.
	require.ErrorIs(t, tool.Finish(ToolResult{Status: OperationStatusOK, Err: context.Canceled}), ErrInvalidObservation)
	require.ErrorIs(t, retrieval.Finish(RetrievalResult{Status: "invalid"}), ErrInvalidObservation)
	require.NoError(t, tool.Close())
	require.NoError(t, retrieval.Close())
	flushTestProvider(t, provider)
	// Assert.
	assert.Empty(t, exporter.GetSpans())
}
