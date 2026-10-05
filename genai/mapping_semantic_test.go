package genai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/metrytest"
)

func TestModelBoundaryKindAndDurationRoute(t *testing.T) {
	for _, test := range []struct {
		name       string
		locality   ExecutionLocality
		completed  bool
		kind       trace.SpanKind
		instrument string
		absent     string
	}{
		{"remote interval", ExecutionRemote, false, trace.SpanKindClient, OperationDurationMetricName, ObservedOperationDurationMetricName},
		{"local interval", ExecutionLocal, false, trace.SpanKindInternal, ObservedOperationDurationMetricName, OperationDurationMetricName},
		{"completed remote fact", ExecutionRemote, true, trace.SpanKindInternal, ObservedOperationDurationMetricName, OperationDurationMetricName},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange.
			tracker, provider, metrics, spans := newTestTrackerWithMetrics(t)
			operation := Operation{Name: "chat", Model: "model", Locality: test.locality}
			result := OperationResult{Status: OperationStatusOK}
			// Act.
			if test.completed {
				result.Duration = time.Second
				require.NoError(t, tracker.Runtime().RecordOperation(context.Background(), operation, result))
			} else {
				_, handle, err := tracker.Runtime().StartOperation(context.Background(), operation, RecordingOptions{})
				require.NoError(t, err)
				require.NoError(t, handle.Finish(result))
			}
			flushTestProvider(t, provider)
			// Assert.
			exported := spans.GetSpans()
			require.Len(t, exported, 1)
			require.Equal(t, "chat model", exported[0].Name)
			require.Equal(t, test.kind, exported[0].SpanKind)
			histogram := metrytest.FindFloat64Histogram(
				t,
				*metrics.LastResourceMetrics(),
				metrytest.MetricSelector{Name: test.instrument},
			)
			require.Len(t, histogram.DataPoints, 1)
			require.EqualValues(t, 1, histogram.DataPoints[0].Count)
			metrytest.AssertMetricAbsent(t, *metrics.LastResourceMetrics(), metrytest.MetricSelector{Name: test.absent})
		})
	}
}

func TestLogicalAccountingDoesNotChangeRemoteBoundary(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, spans := newTestTrackerWithMetrics(t)
	identity := CausalIdentity{RunID: "run", OperationID: "operation", Kind: IdentityLogical}
	// Act.
	_, handle, err := tracker.Runtime().
		StartOperation(context.Background(), Operation{Name: "chat", Locality: ExecutionRemote}, RecordingOptions{Identity: identity})
	require.NoError(t, err)
	require.NoError(
		t,
		handle.Finish(
			OperationResult{
				Status: OperationStatusOK,
				Usage:  Usage{InputTokens: Observe(7, UnitTokens, SourceProvider)},
			},
		),
	)
	flushTestProvider(t, provider)
	// Assert.
	require.Equal(t, trace.SpanKindClient, spans.GetSpans()[0].SpanKind)
	metrytest.AssertMetricAbsent(
		t,
		*metrics.LastResourceMetrics(),
		metrytest.MetricSelector{Name: InputTokenUsageMetricName},
	)
	metrytest.AssertMetricAbsent(
		t,
		*metrics.LastResourceMetrics(),
		metrytest.MetricSelector{Name: OperationDurationMetricName},
	)
}

func TestContractRawReferencePermissionIndependentOfContent(t *testing.T) {
	// Arrange.
	profile := DefaultProfile()
	profile.AllowRaw = true
	profile.PayloadPolicyID = profileRawPolicy
	// Act.
	contract, err := NewContract(
		WithProfile(profile),
		WithPayloadPolicy(RawPayloadPolicy()),
		func(cfg *runtimeConfig) { cfg.recordPayloads = false },
	)
	// Assert.
	require.NoError(t, err)
	require.False(t, contract.RawEnabled())
	require.True(t, contract.ReferenceRawEnabled())
}

func TestInvalidModelMetadataRejectedBeforeWrites(t *testing.T) {
	for _, operation := range []Operation{{Name: "chat", Locality: ExecutionRemote, Model: string([]byte{0xff})}, {Name: "chat", Locality: ExecutionLocal, Purpose: string([]byte{0xff})}} {
		// Arrange.
		tracker, provider, _, spans := newTestTrackerWithMetrics(t)
		// Act.
		_, handle, err := tracker.Runtime().StartOperation(context.Background(), operation, RecordingOptions{})
		_, noopHandle, noopErr := NoopRuntime().StartOperation(context.Background(), operation, RecordingOptions{})
		completedErr := tracker.Runtime().RecordOperation(context.Background(), operation, OperationResult{})
		noopCompletedErr := NoopRuntime().RecordOperation(context.Background(), operation, OperationResult{})
		flushTestProvider(t, provider)
		// Assert.
		require.ErrorIs(t, err, ErrInvalidObservation)
		require.ErrorIs(t, noopErr, ErrInvalidObservation)
		require.ErrorIs(t, completedErr, ErrInvalidObservation)
		require.ErrorIs(t, noopCompletedErr, ErrInvalidObservation)
		require.Nil(t, handle)
		require.Nil(t, noopHandle)
		require.Empty(t, spans.GetSpans())
	}
}
