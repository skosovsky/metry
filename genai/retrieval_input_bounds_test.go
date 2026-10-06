package genai

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetrievalQueryRejectsOversizedInputBeforeRecording(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads())
	request := RetrievalRequest{Locality: ExecutionLocal, Query: strings.Repeat("x", maxPayloadInputBytes+1)}
	// Act.
	_, handle, err := tracker.StartRetrieval(context.Background(), "", request)
	_, noopHandle, noopErr := NoopRecorder().StartRetrieval(context.Background(), "", request)
	// Assert.
	require.ErrorIs(t, err, ErrInvalidObservation)
	require.ErrorIs(t, noopErr, ErrInvalidObservation)
	require.Nil(t, handle)
	require.Nil(t, noopHandle)
	flushTestProvider(t, provider)
	require.Empty(t, exporter.GetSpans())
}
