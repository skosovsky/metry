package genai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
)

func TestNoopAndSamplingPreserveHostBusinessResultAndDiagnostic(t *testing.T) {
	// Arrange: host-owned business work is separate from its telemetry observer.
	tracker, provider, exporter := newTestTrackerWithSampler(t, metry.NeverSample())
	businessErr := errors.New("host failure")
	for _, recorder := range []Recorder{NoopRecorder(), tracker.Recorder()} {
		for _, tokens := range []int{0, -1} {
			// Act.
			result, err, diagnostic := syntheticHostOperation(recorder, tokens, businessErr)
			// Assert.
			assert.Equal(t, "host result", result)
			assert.Same(t, businessErr, err)
			if tokens < 0 {
				require.ErrorIs(t, diagnostic, ErrInvalidObservation)
				var detail *ObservationError
				require.ErrorAs(t, diagnostic, &detail)
				assert.Equal(t, "input_tokens", detail.Field)
			} else {
				require.NoError(t, diagnostic)
			}
		}
	}
	flushTestProvider(t, provider)
	assert.Empty(t, exporter.GetSpans())
}

func syntheticHostOperation(recorder Recorder, tokens int, businessErr error) (string, error, error) {
	result := "host result"
	diagnostic := recorder.RecordOperation(
		context.Background(),
		Operation{Locality: ExecutionRemote, Name: profileChatOperation},
		OperationResult{
			Status: OperationStatusError,
			Usage:  Usage{InputTokens: Observe(tokens, UnitTokens, SourceMeasured)},
		},
	)
	return result, businessErr, diagnostic
}
