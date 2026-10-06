package genai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/metrytest"
)

func TestStandaloneExecutionBoundariesEncloseModelToolRetrievalIO(t *testing.T) {
	// Arrange: ordinary instrumentation requires no causal IDs, experiment
	// metadata, adapter registry or external execution store.
	tracker, provider, metricExporter, exporter := newTestTrackerWithMetrics(t)
	recorder := tracker.Recorder()
	ctx := context.Background()
	// Act: recording starts before each I/O boundary and finishes after it.
	modelCtx, model, err := recorder.StartOperation(
		ctx,
		Operation{Locality: ExecutionRemote, Name: profileChatOperation},
		RecordingOptions{},
	)
	require.NoError(t, err)
	require.True(t, trace.SpanFromContext(modelCtx).IsRecording())
	modelIOStart, modelIOEnd := syntheticIOInterval()
	require.NoError(t, model.Finish(OperationResult{Usage: Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)}}))
	toolCtx, endTool, startErr := recorder.StartTool(ctx, ToolCall{Name: "search"})
	require.NoError(t, startErr)
	require.True(t, trace.SpanFromContext(toolCtx).IsRecording())
	toolIOStart, toolIOEnd := syntheticIOInterval()
	require.NoError(t, endTool.Finish(ToolResult{Status: OperationStatusOK, Result: `{"found":true}`}))
	retrievalCtx, endRetrieval, startErr := tracker.StartRetrieval(
		ctx,
		"",
		RetrievalRequest{Locality: ExecutionRemote, TopK: 1},
	)
	require.NoError(t, startErr)
	require.True(t, trace.SpanFromContext(retrievalCtx).IsRecording())
	retrievalIOStart, retrievalIOEnd := syntheticIOInterval()
	require.NoError(t, endRetrieval.Finish(RetrievalResult{Status: OperationStatusOK, ReturnedDocuments: 1}))
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: exported intervals enclose actual local I/O, not result recording.
	spans := exporter.GetSpans()
	require.Len(t, spans, 3)
	for index, interval := range [][2]time.Time{
		{modelIOStart, modelIOEnd}, {toolIOStart, toolIOEnd}, {retrievalIOStart, retrievalIOEnd},
	} {
		assert.False(t, spans[index].StartTime.After(interval[0]))
		assert.False(t, spans[index].EndTime.Before(interval[1]))
		assert.True(t, spans[index].EndTime.After(spans[index].StartTime))
		assert.False(t, spans[index].Parent.IsValid())
		for _, attr := range spans[index].Attributes {
			assert.NotEqual(t, causalRunKey, string(attr.Key))
		}
	}
	modelDuration := metrytest.FindFloat64Histogram(
		t,
		metrics,
		metrytest.MetricSelector{Name: OperationDurationMetricName},
	)
	require.Len(t, modelDuration.DataPoints, 1)
	assert.Equal(t, uint64(1), modelDuration.DataPoints[0].Count)
	assert.InDelta(t, spans[0].EndTime.Sub(spans[0].StartTime).Seconds(), modelDuration.DataPoints[0].Sum, 1e-9)
	toolDuration := metrytest.FindFloat64Histogram(t, metrics, metrytest.MetricSelector{Name: ToolDurationMetricName})
	require.Len(t, toolDuration.DataPoints, 1)
	assert.Equal(t, uint64(1), toolDuration.DataPoints[0].Count)
	assert.GreaterOrEqual(t, spans[1].EndTime.Sub(spans[1].StartTime).Seconds(), toolDuration.DataPoints[0].Sum)
}

func syntheticIOInterval() (time.Time, time.Time) {
	started := time.Now()
	// A local channel represents a completed synthetic I/O result without a
	// network call, timing sleep or wall-clock subtraction across workers.
	result := make(chan struct{}, 1)
	result <- struct{}{}
	<-result
	return started, time.Now()
}
