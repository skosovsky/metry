package genai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestCausalChildrenRetryAndLogicalAccounting(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t, WithRawPayloads())
	runtime := tracker.Runtime()
	parentIdentity := CausalIdentity{RunID: "parent", OperationID: "request", Kind: IdentityLogical}
	ctx, parent, err := runtime.StartOperation(
		context.Background(),
		Operation{Name: profileChatOperation},
		RecordingOptions{Identity: parentIdentity},
	)
	require.NoError(t, err)
	parentSC := trace.SpanContextFromContext(ctx)
	// Act: two child runs and two physical attempts of one logical operation.
	for _, child := range []string{"child-one", "child-two"} {
		childCtx, childHandle, startErr := runtime.StartOperation(
			ctx,
			Operation{Name: profileChatOperation},
			RecordingOptions{
				Identity: CausalIdentity{
					RunID:       child,
					ParentRunID: "parent",
					OperationID: "model-call",
					Kind:        IdentityLogical,
				},
			},
		)
		require.NoError(t, startErr)
		for _, attemptID := range []string{"attempt-one", "attempt-two"} {
			identity := CausalIdentity{
				RunID:       child,
				ParentRunID: "parent",
				OperationID: "model-call",
				StepID:      "step",
				AttemptID:   attemptID,
				Kind:        IdentityAttempt,
			}
			_, attempt, attemptErr := runtime.StartOperation(
				childCtx,
				Operation{Name: profileChatOperation},
				RecordingOptions{Identity: identity},
			)
			require.NoError(t, attemptErr)
			identity.RunID = "changed-after-start"
			result := OperationResult{
				Usage: Usage{
					InputTokens: Observe(10, UnitTokens, SourceProvider),
					Cost:        Observe(0.1, UnitCurrency, SourceProvider),
					Currency:    "USD",
				},
			}
			if attemptID == "attempt-one" {
				result.Status = OperationStatusError
				result.ErrorType = "cancelled"
			}
			require.NoError(t, attempt.Finish(result))
		}
		require.NoError(
			t,
			childHandle.Finish(OperationResult{Usage: Usage{InputTokens: Observe(20, UnitTokens, SourceProvider)}}),
		)
	}
	require.NoError(
		t,
		parent.Finish(OperationResult{Usage: Usage{InputTokens: Observe(40, UnitTokens, SourceProvider)}}),
	)
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: logical summaries do not multiply physical resource totals.
	spans := traceExporter.GetSpans()
	require.Len(t, spans, 7)
	children, attempts := 0, 0
	for _, span := range spans {
		kind := testutil.SpanStubStringAttr(t, span, causalKindKey)
		if kind == string(IdentityAttempt) {
			attempts++
			assert.NotEqual(t, parentSC.SpanID(), span.Parent.SpanID())
			assert.NotEqual(t, "changed-after-start", testutil.SpanStubStringAttr(t, span, causalRunKey))
		}
		if kind == string(IdentityLogical) && span.Parent.SpanID() == parentSC.SpanID() {
			children++
		}
	}
	assert.Equal(t, 2, children)
	assert.Equal(t, 4, attempts)
	assert.InDelta(
		t,
		40.0,
		metrytest.Int64HistogramSumByAttr(t, metrics, TokenUsageMetricName, TokenType, TokenTypeInput),
		1e-9,
	)
	assert.InDelta(t, 0.4, metrytest.Float64SumValue(t, metrics, CostMetricName), 1e-9)
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			assert.NotContains(t, metric.Name, "parent")
		}
	}
}

func TestOperationHandleConcurrentFinalizationAndValidationRetry(t *testing.T) {
	// Arrange.
	tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t)
	_, handle, err := tracker.Runtime().
		StartOperation(context.Background(), Operation{Name: profileChatOperation}, RecordingOptions{})
	require.NoError(t, err)
	invalid := OperationResult{Usage: Usage{InputTokens: Observe(-1, UnitTokens, SourceMeasured)}}
	// Act: invalid finalization may be corrected; concurrent finalization is local-only.
	require.ErrorIs(t, handle.Finish(invalid), ErrInvalidObservation)
	var successes atomic.Int32
	var duplicates atomic.Int32
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			finishErr := handle.Finish(
				OperationResult{Usage: Usage{InputTokens: Observe(5, UnitTokens, SourceMeasured)}},
			)
			switch {
			case finishErr == nil:
				successes.Add(1)
			case errors.Is(finishErr, ErrFinalized):
				duplicates.Add(1)
			default:
				t.Errorf("unexpected finish error: %v", finishErr)
			}
		})
	}
	workers.Wait()
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert.
	assert.Equal(t, int32(1), successes.Load())
	assert.Equal(t, int32(19), duplicates.Load())
	require.Len(t, traceExporter.GetSpans(), 1)
	assert.InDelta(
		t,
		5.0,
		metrytest.Int64HistogramSumByAttr(t, metrics, TokenUsageMetricName, TokenType, TokenTypeInput),
		1e-9,
	)
	assert.Greater(t, traceExporter.GetSpans()[0].EndTime.Sub(traceExporter.GetSpans()[0].StartTime), time.Duration(0))
}

func TestDetachedResumeLinksCompletedCheckpointAndFanIn(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads())
	identity := CausalIdentity{
		RunID:       "same-run",
		OperationID: "continued",
		StepID:      "checkpoint-step",
		Kind:        IdentityLogical,
	}
	ctx, origin, err := tracker.Runtime().
		StartOperation(context.Background(), Operation{Name: profileChatOperation}, RecordingOptions{Identity: identity})
	require.NoError(t, err)
	originSC := trace.SpanContextFromContext(ctx)
	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	token, err := snapshot.Marshal()
	require.NoError(t, err)
	// Persistence belongs to the host. Business identity travels beside, never
	// inside, the opaque trace token, through an independently serialized envelope.
	envelope := struct {
		Token    string            `json:"token"`
		Identity map[string]string `json:"identity"`
		Policy   string            `json:"policy"`
	}{Token: token, Identity: map[string]string{
		causalRunKey: identity.RunID, causalParentRunKey: identity.ParentRunID,
		causalOperationKey: identity.OperationID, causalStepKey: identity.StepID,
		causalAttemptKey: identity.AttemptID, causalKindKey: string(identity.Kind),
		causalPromptKey: identity.PromptIdentity,
	}, Policy: profileRawPolicy}
	serialized, err := json.Marshal(envelope)
	require.NoError(t, err)
	firstLink, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	require.NoError(t, origin.Finish(OperationResult{}))
	extraCtx, extraEnd, err := provider.StartSpan(context.Background(), "fixture", "cause")
	require.NoError(t, err)
	secondLink, err := metry.NewAsyncHandle(extraCtx)
	require.NoError(t, err)
	extraEnd()
	var restored struct {
		Token    string            `json:"token"`
		Identity map[string]string `json:"identity"`
		Policy   string            `json:"policy"`
	}
	require.NoError(t, json.Unmarshal(serialized, &restored))
	restoredIdentity := CausalIdentity{
		RunID: restored.Identity[causalRunKey], ParentRunID: restored.Identity[causalParentRunKey],
		OperationID: restored.Identity[causalOperationKey], StepID: restored.Identity[causalStepKey],
		AttemptID: restored.Identity[causalAttemptKey], Kind: IdentityKind(restored.Identity[causalKindKey]),
		PromptIdentity: restored.Identity[causalPromptKey],
	}
	restoredSnapshot, err := metry.ParseTraceSnapshot(restored.Token)
	require.NoError(t, err)
	workerCtx, err := provider.ContextWithTraceSnapshot(context.Background(), restoredSnapshot)
	require.NoError(t, err)
	links := []metry.AsyncHandle{firstLink, secondLink}
	// Act.
	_, resumed, err := tracker.Runtime().
		StartOperation(workerCtx, Operation{Name: profileChatOperation}, RecordingOptions{Identity: restoredIdentity, Detached: true, Links: links, PolicyID: restored.Policy})
	require.NoError(t, err)
	links[0] = metry.AsyncHandle{}
	require.NoError(t, resumed.Finish(OperationResult{}))
	flushTestProvider(t, provider)
	// Assert.
	spans := exporter.GetSpans()
	require.Len(t, spans, 3)
	span := spans[2]
	assert.False(t, span.Parent.IsValid())
	assert.NotEqual(t, originSC.TraceID(), span.SpanContext.TraceID())
	require.Len(t, span.Links, 2)
	assert.Equal(t, originSC.SpanID(), span.Links[0].SpanContext.SpanID())
	assert.Equal(t, "same-run", testutil.SpanStubStringAttr(t, span, causalRunKey))
	assert.Equal(t, identity, restoredIdentity)
	for _, key := range []string{causalRunKey, causalOperationKey, causalStepKey, causalKindKey} {
		assert.Equal(t, testutil.SpanStubStringAttr(t, spans[0], key), testutil.SpanStubStringAttr(t, span, key))
	}
	assert.Equal(t, string(PredecessorUnknown), testutil.SpanStubStringAttr(t, span, "metry.causal.predecessor"))
}

func TestMalformedCausalObservationsDoNotStartSpans(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	options := []RecordingOptions{
		{Identity: CausalIdentity{RunID: "secret-only-id"}},
		{Identity: CausalIdentity{RunID: "run", OperationID: "op", Kind: IdentityAttempt}},
		{Identity: CausalIdentity{RunID: strings.Repeat("secret", 100), OperationID: "op", Kind: IdentityLogical}},
		{Links: []metry.AsyncHandle{{}}},
		{Mapping: "secret-mapping"},
		{PolicyID: "secret-policy"},
	}
	// Act and Assert.
	for _, option := range options {
		_, handle, err := tracker.Runtime().
			StartOperation(context.Background(), Operation{Name: profileChatOperation}, option)
		require.Error(t, err)
		assert.Nil(t, handle)
		assert.NotContains(t, err.Error(), "secret")
	}
	flushTestProvider(t, provider)
	assert.Empty(t, exporter.GetSpans())
}

func TestNoopOperationHandleHasNoExecutionDecisions(t *testing.T) {
	// Arrange.
	ctx := context.Background()
	// Act.
	gotContext, handle, err := NoopRuntime().StartOperation(ctx, Operation{Name: profileChatOperation}, RecordingOptions{})
	// Assert.
	require.NoError(t, err)
	assert.Equal(t, ctx, gotContext)
	require.NoError(t, handle.Finish(OperationResult{}))
	require.ErrorIs(t, handle.Finish(OperationResult{}), ErrFinalized)
}
