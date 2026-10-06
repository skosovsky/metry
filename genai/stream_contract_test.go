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

func TestStreamUpdatesCommitOnlyFinalUsage(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	_, handle, err := tracker.Recorder().
		StartOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: profileChatOperation}, RecordingOptions{})
	require.NoError(t, err)
	// Act: cumulative updates do not add counters or infer final usage.
	for _, count := range []int{2, 5, 10, 10} {
		require.NoError(
			t,
			handle.ValidateStreamingUpdate(Usage{OutputTokens: Observe(count, UnitTokens, SourceProvider)}),
		)
	}
	require.NoError(
		t,
		handle.Finish(
			OperationResult{
				Usage: Usage{
					OutputTokens: Observe(10, UnitTokens, SourceProvider),
					Cost:         Observe(0.5, UnitCurrency, SourceProvider),
					Currency:     "USD",
				},
				Streaming: &StreamTimings{TTFT: 0, FirstTokenPresent: true},
			},
		),
	)
	require.ErrorIs(t, handle.Finish(OperationResult{}), ErrFinalized)
	require.ErrorIs(t, handle.ValidateStreamingUpdate(Usage{}), ErrFinalized)
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert.
	require.Len(t, traceExporter.GetSpans(), 1)
	assert.InDelta(
		t,
		10.0,
		testInt64HistogramSum(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName}),
		1e-9,
	)
	assert.InDelta(t, 0.5, metrytest.Float64SumValue(t, metrics, metrytest.MetricSelector{Name: CostMetricName}), 1e-9)
	histogram := metrytest.FindInt64Histogram(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	require.Len(t, histogram.DataPoints, 1)
	assert.Equal(t, uint64(1), histogram.DataPoints[0].Count)
	ttft := metrytest.FindFloat64Histogram(t, metrics, metrytest.MetricSelector{Name: TTFTMetricName})
	require.Len(t, ttft.DataPoints, 1)
	assert.Equal(t, uint64(1), ttft.DataPoints[0].Count)
	assert.InDelta(t, 0.0, ttft.DataPoints[0].Sum, 1e-9)
}

func TestAbortedStreamDoesNotInferMissingTotal(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	_, handle, err := tracker.Recorder().
		StartOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: profileChatOperation}, RecordingOptions{})
	require.NoError(t, err)
	// Act.
	require.NoError(t, handle.ValidateStreamingUpdate(Usage{OutputTokens: Observe(100, UnitTokens, SourceProvider)}))
	require.NoError(
		t,
		handle.Finish(
			OperationResult{Status: OperationStatusError, ErrorType: "stream_aborted", Streaming: &StreamTimings{}},
		),
	)
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert.
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: CostMetricName})
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: TTFTMetricName})
	span := traceExporter.GetSpans()[0]
	assert.False(t, testutil.SpanStubHasAttr(span, OutputTokens))
}

func TestInvalidStreamIntervalsRejectBeforeRecording(t *testing.T) {
	for _, event := range []StreamingCompletion{
		{TTFT: -time.Second, TotalDuration: time.Second, FirstTokenPresent: true},
		{TTFT: time.Second, TotalDuration: 0, FirstTokenPresent: true},
		{TTFT: time.Second, TotalDuration: time.Second, FirstTokenPresent: false},
		{TotalDuration: -time.Second},
	} {
		// Arrange.
		tracker, provider, metricExporter, _ := newTestTrackerWithMetrics(t)
		event.Meta.Operation = profileChatOperation
		event.Usage = Usage{InputTokens: Observe(10, UnitTokens, SourceMeasured)}
		// Act.
		err := tracker.Recorder().RecordStreamingCompletion(context.Background(), event)
		metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
		// Assert.
		require.ErrorIs(t, err, ErrInvalidObservation)
		metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
		metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
		metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: OperationDurationMetricName})
		metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: ObservedOperationDurationMetricName})
	}
}

func TestInvalidFinalTimingCanBeCorrected(t *testing.T) {
	// Arrange.
	tracker, _, _ := newTestTracker(t)
	_, handle, err := tracker.Recorder().
		StartOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: profileChatOperation}, RecordingOptions{})
	require.NoError(t, err)
	// Act.
	err = handle.Finish(OperationResult{Streaming: &StreamTimings{TTFT: time.Hour, FirstTokenPresent: true}})
	// Assert.
	require.ErrorIs(t, err, ErrInvalidObservation)
	require.NoError(t, handle.Finish(OperationResult{Streaming: &StreamTimings{}}))
}

func TestCompletedOperationValidatesTimingsIncludingNoop(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	invalid := []OperationResult{
		{Duration: -time.Second},
		{Duration: time.Second, Streaming: &StreamTimings{TTFT: 2 * time.Second, FirstTokenPresent: true}},
		{Duration: time.Second, Usage: Usage{OutputTokens: Observe(-1, UnitTokens, SourceProvider)}},
	}
	// Act.
	for _, result := range invalid {
		require.ErrorIs(t, tracker.Recorder().RecordOperation(
			context.Background(),
			Operation{Locality: ExecutionRemote, Name: profileChatOperation}, result),
			ErrInvalidObservation)
		require.ErrorIs(t, NoopRecorder().RecordOperation(
			context.Background(),
			Operation{Locality: ExecutionRemote, Name: profileChatOperation}, result),
			ErrInvalidObservation)
	}
	require.ErrorIs(t, tracker.RecordInteraction(context.Background(),
		Meta{Duration: -time.Second}, Payload{}, Usage{}), ErrInvalidObservation)
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: invalid completed observations don't create even partial writes.
	require.Empty(t, traceExporter.GetSpans())
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: OperationDurationMetricName})
	metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: ObservedOperationDurationMetricName})
}

func TestCompletedOperationEmitsFinalStreamingQuality(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, _ := newTestTrackerWithMetrics(t)
	result := OperationResult{
		Duration:  3 * time.Second,
		Usage:     Usage{OutputTokens: Observe(10, UnitTokens, SourceProvider)},
		Streaming: &StreamTimings{TTFT: time.Second, FirstTokenPresent: true},
	}
	// Act.
	require.NoError(t, tracker.Recorder().RecordOperation(
		context.Background(), Operation{Locality: ExecutionRemote, Name: profileChatOperation}, result))
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: the completed DTO path doesn't silently ignore streaming metadata.
	assert.InDelta(
		t,
		1,
		metrytest.Float64HistogramSum(t, metrics, metrytest.MetricSelector{Name: TTFTMetricName}),
		1e-9,
	)
	assert.InDelta(
		t,
		5,
		metrytest.Float64HistogramSum(
			t,
			metrics,
			metrytest.MetricSelector{Name: DerivedOutputTokensPerSecondMetricName},
		),
		1e-9,
	)
	assert.InDelta(
		t,
		10,
		testInt64HistogramSum(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName}),
		1e-9,
	)
}
