package genai

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestModelOutcomeMatrix(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  Outcome
		class   string
		want    codes.Code
		invalid bool
	}{
		{"success", OperationStatusOK, "", codes.Ok, false},
		{"unknown", "", "", codes.Unset, false},
		{"unset", OperationStatusUnset, "", codes.Unset, false},
		{"error", OperationStatusError, "", codes.Error, false},
		{"cancelled", "", "cancelled", codes.Error, false},
		{"deadline", "", "deadline_exceeded", codes.Error, false},
		{"abort", "", "stream_aborted", codes.Error, false},
		{"conflict", OperationStatusOK, "internal", codes.Unset, true},
		{"unset_conflict", OperationStatusUnset, "internal", codes.Unset, true},
		{"invalid", "whatever", "", codes.Unset, true},
		{"private_class", "", "secret_marker", codes.Unset, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange.
			tracker, provider, mem := newTestTracker(t, WithProfile(observedModelProfile()), WithRawPayloads())
			ctx := WithScope(context.Background(), Scope{Model: "router-default"})
			_, handle, err := tracker.Recorder().
				StartOperation(ctx, Operation{Locality: ExecutionRemote, Name: "chat"}, RecordingOptions{})
			require.NoError(t, err)
			result := OperationResult{Status: test.status, ErrorType: test.class}
			// Act.
			err = handle.Finish(result)
			noopErr := NoopRecorder().RecordOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: "chat"}, result)
			// Assert.
			if test.invalid {
				require.ErrorIs(t, err, ErrInvalidObservation)
				require.ErrorIs(t, noopErr, ErrInvalidObservation)
				assert.NotContains(t, err.Error(), "secret_marker")
				require.NoError(t, handle.Close())
			} else {
				require.NoError(t, err)
				require.NoError(t, noopErr)
			}
			flushTestProvider(t, provider)
			spans := mem.GetSpans()
			require.Len(t, spans, 1)
			assert.Equal(t, test.want, spans[0].Status.Code)
			assert.Equal(t, "router-default", testutil.SpanStubStringAttr(t, spans[0], RequestModel))
			for _, attr := range spans[0].Attributes {
				assert.NotEqual(t, ResponseModel, string(attr.Key))
			}
			require.ErrorIs(t, handle.Close(), ErrFinalized)
		})
	}
}

func TestModelActualMetadataAndConcurrentFinish(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, spans := newTestTrackerWithMetrics(
		t,
		WithProfile(observedModelProfile()),
		WithRawPayloads(),
	)
	_, handle, err := tracker.Recorder().
		StartOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: "chat", Model: "router-default"}, RecordingOptions{})
	require.NoError(t, err)
	var committed atomic.Int32
	var wg sync.WaitGroup
	// Act.
	for range 20 {
		wg.Go(func() {
			if err := handle.Finish(
				OperationResult{
					Status:   OperationStatusOK,
					Response: ResponseMetadata{Model: "model-b"},
					Usage:    Usage{InputTokens: Observe(7, UnitTokens, SourceMeasured)},
				},
			); err == nil {
				committed.Add(1)
			} else {
				assert.Equal(t, ErrFinalized, err)
			}
		})
	}
	wg.Wait()
	// Assert.
	assert.Equal(t, int32(1), committed.Load())
	flushTestProvider(t, provider)
	exported := spans.GetSpans()
	require.Len(t, exported, 1)
	assert.Equal(t, "router-default", testutil.SpanStubStringAttr(t, exported[0], RequestModel))
	assert.Equal(t, "model-b", testutil.SpanStubStringAttr(t, exported[0], ResponseModel))
	rm := metrytest.CollectResourceMetrics(t, provider, metrics)
	assert.InDelta(
		t,
		float64(7),
		metrytest.Int64HistogramSumByAttr(
			t,
			rm,
			metrytest.MetricSelector{Name: InputTokenUsageMetricName},
			RequestModel,
			"router-default",
		),
		1e-9,
	)
}

func TestCloseDoesNotInventUsage(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, spans := newTestTrackerWithMetrics(
		t,
		WithProfile(observedModelProfile()),
		WithRawPayloads(),
	)
	_, handle, err := tracker.Recorder().
		StartOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: "chat", Model: "alias"}, RecordingOptions{})
	require.NoError(t, err)
	require.NoError(t, handle.ValidateStreamingUpdate(Usage{InputTokens: Observe(99, UnitTokens, SourceMeasured)}))
	// Act.
	require.NoError(t, handle.Close())
	// Assert.
	flushTestProvider(t, provider)
	require.Len(t, spans.GetSpans(), 1)
	assert.Equal(t, codes.Unset, spans.GetSpans()[0].Status.Code)
	rm := metrytest.CollectResourceMetrics(t, provider, metrics)
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	var zero OperationHandle
	require.ErrorIs(t, zero.Close(), ErrInvalidObservation)
	require.ErrorIs(t, zero.Finish(OperationResult{}), ErrInvalidObservation)
	require.ErrorIs(t, handle.Finish(OperationResult{}), ErrFinalized)
}

func observedModelProfile() Profile {
	profile := testInstrumentationProfile()
	for index := range profile.Dimensions {
		switch profile.Dimensions[index].Key {
		case RequestModel:
			profile.Dimensions[index].Values = []string{"router-default", "alias"}
		case ResponseModel:
			profile.Dimensions[index].Values = []string{"model-b"}
		}
	}
	return profile
}

func TestModelFinishVersusClose(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, spans := newTestTrackerWithMetrics(t)
	_, handle, err := tracker.Recorder().
		StartOperation(context.Background(), Operation{Locality: ExecutionRemote, Name: "chat"}, RecordingOptions{})
	require.NoError(t, err)
	var committed atomic.Int32
	var wg sync.WaitGroup
	// Act.
	for index := range 20 {
		wg.Go(func() {
			var finishErr error
			if index%2 == 0 {
				finishErr = handle.Close()
			} else {
				finishErr = handle.Finish(
					OperationResult{
						Status: OperationStatusOK,
						Usage:  Usage{InputTokens: Observe(5, UnitTokens, SourceMeasured)},
					},
				)
			}
			if finishErr == nil {
				committed.Add(1)
			} else {
				assert.Equal(t, ErrFinalized, finishErr)
			}
		})
	}
	wg.Wait()
	// Assert.
	assert.Equal(t, int32(1), committed.Load())
	flushTestProvider(t, provider)
	require.Len(t, spans.GetSpans(), 1)
	rm := metrytest.CollectResourceMetrics(t, provider, metrics)
	if spans.GetSpans()[0].Status.Code == codes.Unset {
		metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
		metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	} else {
		assert.InDelta(
			t,
			float64(5),
			testInt64HistogramSum(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName}),
			1e-9,
		)
	}
}

func TestModelMetadataAcrossCompletedRoutes(t *testing.T) {
	for _, route := range []string{"completed", "streaming", "adapter", "helper"} {
		for _, actual := range []string{"model-b", ""} {
			t.Run(route+"/"+actual, func(t *testing.T) {
				// Arrange.
				tracker, provider, metrics, spans := newTestTrackerWithMetrics(
					t,
					WithProfile(observedModelProfile()),
					WithRawPayloads(),
				)
				ctx := WithScope(context.Background(), Scope{Model: "router-default"})
				meta := Meta{Operation: "chat", Status: OperationStatusOK, ResponseModel: actual}
				// Act.
				var err error
				switch route {
				case "completed":
					err = tracker.Recorder().
						RecordOperation(ctx, Operation{Locality: ExecutionRemote, Name: "chat", Model: "router-default"}, OperationResult{Status: OperationStatusOK, Response: ResponseMetadata{Model: actual}, Usage: Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)}})
				case "streaming":
					err = tracker.RecordStreamingCompletion(
						ctx,
						StreamingCompletion{
							Meta:  meta,
							Usage: Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)},
						},
					)
				case "adapter":
					err = tracker.RecordModelInteraction(ctx, task19ResponseAdapter{actual: actual}, nil, nil)
				case "helper":
					err = tracker.RecordInteraction(
						ctx,
						meta,
						Payload{},
						Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)},
					)
				}
				// Assert.
				require.NoError(t, err)
				flushTestProvider(t, provider)
				exported := spans.GetSpans()
				require.Len(t, exported, 1)
				assert.Equal(t, "router-default", testutil.SpanStubStringAttr(t, exported[0], RequestModel))
				if actual != "" {
					assert.Equal(t, actual, testutil.SpanStubStringAttr(t, exported[0], ResponseModel))
				} else {
					assert.False(t, testutil.SpanStubHasAttr(exported[0], ResponseModel))
				}
				rm := metrytest.CollectResourceMetrics(t, provider, metrics)
				histogram := metrytest.FindInt64Histogram(
					t,
					rm,
					metrytest.MetricSelector{Name: InputTokenUsageMetricName},
				)
				require.Len(t, histogram.DataPoints, 1)
				requestValue, ok := histogram.DataPoints[0].Attributes.Value(RequestModel)
				require.True(t, ok)
				assert.Equal(t, "router-default", requestValue.AsString())
				responseValue, ok := histogram.DataPoints[0].Attributes.Value(ResponseModel)
				assert.Equal(t, actual != "", ok)
				if ok {
					assert.Equal(t, actual, responseValue.AsString())
				}
			})
		}
	}
}

type task19ResponseAdapter struct{ actual string }

func (task19ResponseAdapter) ParseRequest(any) (Payload, Meta, error) {
	return Payload{}, Meta{Operation: "chat", ResponseModel: "fabricated-request-value"}, nil
}
func (a task19ResponseAdapter) ParseResponse(any) (ResponseObservation, error) {
	return ResponseObservation{
		Metadata: ResponseMetadata{Model: a.actual},
		Status:   OperationStatusOK,
		Usage:    Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)},
	}, nil
}

func TestFinalizedModelRejectsAllFurtherWrites(t *testing.T) {
	for _, terminal := range []string{"finish", "close"} {
		for _, noop := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/noop=%t", terminal, noop), func(t *testing.T) {
				// Arrange.
				recorder := NoopRecorder()
				if !noop {
					tracker, _, _ := newTestTracker(t)
					recorder = tracker.Recorder()
				}
				_, handle, err := recorder.StartOperation(
					context.Background(),
					Operation{Locality: ExecutionRemote, Name: "chat"},
					RecordingOptions{},
				)
				require.NoError(t, err)
				// Act.
				if terminal == "close" {
					require.NoError(t, handle.Close())
				} else {
					require.NoError(t, handle.Finish(OperationResult{Status: OperationStatusOK}))
				}
				// Assert: finalization takes precedence even over invalid DTO validation.
				require.ErrorIs(t, handle.Finish(OperationResult{Status: "invalid"}), ErrFinalized)
				require.ErrorIs(t, handle.Finish(OperationResult{Duration: time.Second}), ErrFinalized)
				require.ErrorIs(
					t,
					handle.ValidateStreamingUpdate(Usage{InputTokens: Observe(-1, UnitTokens, SourceMeasured)}),
					ErrFinalized,
				)
				require.ErrorIs(t, handle.Close(), ErrFinalized)
			})
		}
	}
}
