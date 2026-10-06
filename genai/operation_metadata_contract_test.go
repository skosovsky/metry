package genai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestCompletedObservationsRequireEffectiveOperationBeforeAnyWrites(t *testing.T) {
	for _, name := range []string{"", "   ", strings.Repeat("secret", maxIdentityBytes), string([]byte{0xff})} {
		// Arrange.
		tracker, provider, metricExporter, exporter := newTestTrackerWithMetrics(t)
		usage := Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)}
		// Act.
		err := tracker.Recorder().
			RecordOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: name}, OperationResult{Usage: usage})
		interactionErr := tracker.RecordInteraction(context.Background(), Meta{Operation: name}, Payload{}, usage)
		noopErr := NoopRecorder().RecordOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: name}, OperationResult{Usage: usage})
		flushTestProvider(t, provider)
		metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
		// Assert.
		for _, diagnostic := range []error{err, interactionErr, noopErr} {
			require.ErrorIs(t, diagnostic, ErrInvalidObservation)
			assert.NotContains(t, diagnostic.Error(), "secret")
		}
		assert.Empty(t, exporter.GetSpans())
		metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
		metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	}
}

func TestStreamingCompletionRequiresEffectiveOperationBeforeMetrics(t *testing.T) {
	for _, name := range []string{"", "   ", strings.Repeat("secret", maxIdentityBytes), string([]byte{0xff})} {
		// Arrange.
		tracker, provider, metricExporter, exporter := newTestTrackerWithMetrics(t)
		event := StreamingCompletion{Meta: Meta{Operation: name},
			Usage:             Usage{OutputTokens: Observe(0, UnitTokens, SourceMeasured)},
			FirstTokenPresent: true, TotalDuration: time.Second}
		// Act.
		diagnostics := []error{
			tracker.RecordStreamingCompletion(context.Background(), event),
			tracker.Recorder().RecordStreamingCompletion(context.Background(), event),
			NoopRecorder().RecordStreamingCompletion(context.Background(), event),
			(trackerRecorder{}).RecordStreamingCompletion(context.Background(), event),
		}
		flushTestProvider(t, provider)
		metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
		// Assert: even known-zero and TTFT do not partially commit.
		for _, diagnostic := range diagnostics {
			require.ErrorIs(t, diagnostic, ErrInvalidObservation)
			assert.NotContains(t, diagnostic.Error(), "secret")
		}
		assert.Empty(t, exporter.GetSpans())
		for _, instrument := range []string{InputTokenUsageMetricName, ObservedOperationDurationMetricName,
			TTFTMetricName, DerivedOutputTokensPerSecondMetricName, DerivedMeanOutputTokenIntervalMetricName} {
			metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: instrument})
		}
	}
}

func TestStreamingCompletionAcceptsCallerScopeOperation(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, _ := newTestTrackerWithMetrics(t)
	ctx := WithScope(context.Background(), Scope{Operation: profileChatOperation})
	event := StreamingCompletion{Usage: Usage{OutputTokens: Observe(0, UnitTokens, SourceMeasured)},
		FirstTokenPresent: true, TotalDuration: time.Second}
	// Act.
	require.NoError(t, tracker.Recorder().RecordStreamingCompletion(ctx, event))
	require.NoError(t, NoopRecorder().RecordStreamingCompletion(ctx, event))
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: caller metadata permits a measured-zero observation once.
	tokens := metrytest.FindInt64Histogram(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	require.Len(t, tokens.DataPoints, 1)
	assert.Equal(t, uint64(1), tokens.DataPoints[0].Count)
	assert.Zero(t, tokens.DataPoints[0].Sum)
	assert.Zero(t, metrytest.Float64HistogramSum(t, metrics, metrytest.MetricSelector{Name: TTFTMetricName}))
}

func TestCompletedObservationsAcceptCallerDeclaredScopeOperation(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	ctx := WithScope(context.Background(), Scope{Operation: profileChatOperation})
	// Act.
	require.NoError(t, tracker.Recorder().RecordOperation(ctx, Operation{Locality: ExecutionRemote}, OperationResult{}))
	require.NoError(t, tracker.RecordInteraction(ctx, Meta{}, Payload{}, Usage{}))
	require.NoError(t, NoopRecorder().RecordOperation(ctx, Operation{Locality: ExecutionRemote}, OperationResult{}))
	flushTestProvider(t, provider)
	// Assert: a scope value is caller-owned metadata, not a synthetic fallback.
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	for _, span := range spans {
		assert.Equal(t, profileChatOperation, testutil.SpanStubStringAttr(t, span, OperationName))
	}
}
