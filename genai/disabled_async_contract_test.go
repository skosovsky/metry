package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
)

func TestDisabledAsyncHandleStaysDisabledAcrossRecordingBoundaries(t *testing.T) {
	// Arrange.
	tracker, provider, exports := newTestTracker(t)
	disabled, err := NoopRecorder().StartAsync(context.Background(), "disabled")
	require.NoError(t, err)
	reference, err := tracker.Recorder().StartAsync(context.Background(), "real-origin")
	require.NoError(t, err)
	// Act: enabled emission cannot turn an opt-out into a causal reference.
	require.NoError(t, tracker.Recorder().RecordAsyncResult(
		context.Background(), disabled, AsyncResult{Name: "ignored"}))
	require.NoError(t, tracker.RecordAsyncFeedback(context.Background(), disabled, 0, "ignored"))
	require.ErrorIs(t, tracker.RecordAsyncFeedback(context.Background(), metry.AsyncHandle{}, 0, "ignored"),
		ErrInvalidAsyncHandle)
	_, operation, err := tracker.Recorder().StartOperation(context.Background(),
		Operation{Name: "chat", Locality: ExecutionRemote},
		RecordingOptions{Links: []metry.AsyncHandle{disabled, reference, disabled}})
	require.NoError(t, err)
	require.NoError(t, operation.Close())
	_, agent, err := tracker.Recorder().StartAgent(context.Background(),
		AgentInvocation{Name: "agent", Locality: ExecutionLocal},
		RecordingOptions{Links: []metry.AsyncHandle{disabled}})
	require.NoError(t, err)
	require.NoError(t, agent.Close())
	evaluation := successfulEvaluation()
	evaluation.TraceReference = &disabled
	evaluationErr := tracker.EvaluationRecorder().Record(context.Background(), evaluation)
	flushTestProvider(t, provider)
	// Assert: real references survive, disabled references never become links.
	require.False(t, disabled.IsValid())
	token, err := disabled.Marshal()
	require.ErrorIs(t, err, metry.ErrInvalidAsyncHandle)
	require.Empty(t, token)
	require.ErrorIs(t, evaluationErr, ErrInvalidObservation)
	spans := exports.GetSpans()
	require.Len(t, spans, 3)
	require.Empty(t, spans[0].Links)
	require.Len(t, spans[1].Links, 1)
	require.Equal(t, spans[0].SpanContext.TraceID(), spans[1].Links[0].SpanContext.TraceID())
	require.Equal(t, spans[0].SpanContext.SpanID(), spans[1].Links[0].SpanContext.SpanID())
	require.Empty(t, spans[2].Links)
}
