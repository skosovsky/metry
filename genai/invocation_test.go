package genai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestInvocationLocalHostedAndWorkflowBoundaries(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, exported := newTestTrackerWithMetrics(t, WithRawPayloads())
	ctx, workflow, err := tracker.StartWorkflow(
		context.Background(),
		WorkflowInvocation{Name: "pipeline", ID: "workflow-id", Version: "v1", ConversationID: "conversation"},
		RecordingOptions{},
	)
	require.NoError(t, err)
	// Act.
	_, local, err := tracker.Recorder().
		StartAgent(ctx, AgentInvocation{Name: "local", ID: "local-id", Locality: ExecutionLocal}, RecordingOptions{})
	require.NoError(t, err)
	require.NoError(t, local.Finish(InvocationResult{Status: OperationStatusOK}))
	_, hosted, err := tracker.Recorder().
		ForProvider("openai").
		StartAgent(ctx, AgentInvocation{Name: "hosted", ID: "asst-1", Version: "v2", ConversationID: "conversation", Locality: ExecutionRemote}, RecordingOptions{Identity: CausalIdentity{RunID: "run", OperationID: "operation", Kind: IdentityLogical}})
	require.NoError(t, err)
	require.NoError(t, hosted.Close())
	require.NoError(t, workflow.Finish(InvocationResult{Status: OperationStatusOK}))
	flushTestProvider(t, provider)
	// Assert: logical accounting never changes a remote execution boundary.
	spans := exported.GetSpans()
	require.Len(t, spans, 3)
	w := testutil.SpanByName(t, spans, "invoke_workflow pipeline")
	l := testutil.SpanByName(t, spans, "invoke_agent local")
	h := testutil.SpanByName(t, spans, "invoke_agent hosted")
	assert.Equal(t, trace.SpanKindInternal, w.SpanKind)
	assert.Equal(t, trace.SpanKindInternal, l.SpanKind)
	assert.Equal(t, trace.SpanKindClient, h.SpanKind)
	assert.Equal(t, w.SpanContext.SpanID(), l.Parent.SpanID())
	assert.Equal(t, w.SpanContext.SpanID(), h.Parent.SpanID())
	assert.Equal(t, "local-id", testutil.SpanStubStringAttr(t, l, LocalAgentID))
	assert.False(t, testutil.SpanStubHasAttr(l, AgentID))
	assert.Equal(t, "asst-1", testutil.SpanStubStringAttr(t, h, AgentID))
	assert.Equal(t, "openai", testutil.SpanStubStringAttr(t, h, ProviderName))
	assert.Equal(t, "v2", testutil.SpanStubStringAttr(t, h, AgentVersion))
	assert.Equal(t, "conversation", testutil.SpanStubStringAttr(t, h, ConversationID))
	assert.Equal(t, OperationStatusUnset, testutil.SpanStubStringAttr(t, h, OperationStatus))
	rm := metrytest.CollectResourceMetrics(t, provider, metrics)
	hist := metrytest.FindFloat64Histogram(t, rm, metrytest.MetricSelector{Name: InvocationDurationMetricName})
	var count uint64
	for _, point := range hist.DataPoints {
		count += point.Count
		assert.Equal(t, 3, point.Attributes.Len())
	}
	assert.Equal(t, uint64(3), count)
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: OperationDurationMetricName})
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	metrytest.AssertMetricAbsent(t, rm, metrytest.MetricSelector{Name: CostMetricName})
}

func TestInvocationRejectsBeforeWritesAndNoopParity(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	for _, recorder := range []Recorder{tracker.Recorder(), NoopRecorder()} {
		for _, invocation := range []AgentInvocation{
			{Name: "agent"},
			{Name: "agent", Locality: "unsupported"},
			{Name: "bad name", Locality: ExecutionLocal},
			{Name: strings.Repeat("a", 81), Locality: ExecutionLocal},
			{Name: "agent", ID: strings.Repeat("x", 257), Locality: ExecutionLocal},
			{Name: "agent", Version: "bad\nversion", Locality: ExecutionLocal},
			{Name: "agent", ConversationID: string([]byte{0xff}), Locality: ExecutionLocal},
		} {
			// Act.
			_, handle, err := recorder.StartAgent(context.Background(), invocation, RecordingOptions{})
			// Assert.
			require.ErrorIs(t, err, ErrInvalidObservation)
			require.Nil(t, handle)
		}
		_, handle, err := recorder.StartWorkflow(
			context.Background(),
			WorkflowInvocation{Name: "workflow"},
			RecordingOptions{Predecessor: "unsupported"},
		)
		require.ErrorIs(t, err, ErrInvalidObservation)
		require.Nil(t, handle)
	}
	flushTestProvider(t, provider)
	require.Empty(t, exporter.GetSpans())
}

func TestInvocationFinalizationOutcomesAndRace(t *testing.T) {
	for _, noop := range []bool{false, true} {
		t.Run(fmt.Sprintf("noop=%t", noop), func(t *testing.T) {
			// Arrange.
			tracker, provider, exporter := newTestTracker(t)
			recorder := tracker.Recorder()
			if noop {
				recorder = NoopRecorder()
			}
			_, handle, err := recorder.StartAgent(
				context.Background(),
				AgentInvocation{Name: "agent", Locality: ExecutionLocal},
				RecordingOptions{},
			)
			require.NoError(t, err)
			// Act: invalid result stays correctable; one of concurrent callers wins.
			require.ErrorIs(
				t,
				handle.Finish(InvocationResult{Status: OperationStatusOK, Err: errors.New("SECRET")}),
				ErrInvalidObservation,
			)
			var winners atomic.Int64
			var group sync.WaitGroup
			for range 32 {
				group.Go(func() {
					finishErr := handle.Finish(InvocationResult{Err: context.Canceled})
					if finishErr == nil {
						winners.Add(1)
					} else {
						assert.ErrorIs(t, finishErr, ErrFinalized)
					}
				})
			}
			group.Wait()
			// Assert: finalized takes precedence over malformed future result.
			require.Equal(t, int64(1), winners.Load())
			require.ErrorIs(t, handle.Finish(InvocationResult{Status: "invalid"}), ErrFinalized)
			require.ErrorIs(t, handle.Close(), ErrFinalized)
			flushTestProvider(t, provider)
			if noop {
				require.Empty(t, exporter.GetSpans())
			} else {
				spans := exporter.GetSpans()
				require.Len(t, spans, 1)
				assert.Equal(t, string(ErrorClassCancelled), testutil.SpanStubStringAttr(t, spans[0], ErrorType))
				assert.Equal(t, OperationStatusError, testutil.SpanStubStringAttr(t, spans[0], OperationStatus))
			}
		})
	}
}

func TestInvocationDetachedFanInAndProtectedReferences(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t)
	ctx, parent, err := tracker.StartWorkflow(
		context.Background(),
		WorkflowInvocation{Name: "SECRET-workflow", ConversationID: "SECRET-conversation"},
		RecordingOptions{},
	)
	require.NoError(t, err)
	origin, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	// Act.
	_, resumed, err := tracker.StartAgent(
		ctx,
		AgentInvocation{Name: "SECRET-agent", ID: "SECRET-id", Version: "SECRET-version", Locality: ExecutionLocal},
		RecordingOptions{Detached: true, Links: []metry.AsyncHandle{origin, origin}},
	)
	require.NoError(t, err)
	require.NoError(t, resumed.Close())
	require.NoError(t, parent.Close())
	flushTestProvider(t, provider)
	// Assert: references protected independently of content recording.
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	for _, span := range spans {
		assert.NotContains(t, span.Name, "SECRET")
		for _, attr := range span.Attributes {
			assert.NotContains(t, attr.Value.String(), "SECRET")
		}
		if strings.HasPrefix(span.Name, "invoke_agent ") {
			assert.False(t, span.Parent.IsValid())
			require.Len(t, span.Links, 1)
			assert.Equal(t, trace.SpanContextFromContext(ctx).SpanID(), span.Links[0].SpanContext.SpanID())
		}
	}
}

func TestInvocationMetricDimensionsIgnoreThousandsOfReferences(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, _ := newTestTrackerWithMetrics(t)
	// Act.
	for i := range 1200 {
		id := fmt.Sprintf("run-%d", i)
		_, handle, err := tracker.StartAgent(
			context.Background(),
			AgentInvocation{
				Name:           fmt.Sprintf("agent-%d", i),
				ID:             id,
				Version:        id,
				ConversationID: id,
				Locality:       ExecutionLocal,
			},
			RecordingOptions{Identity: CausalIdentity{RunID: id, OperationID: id, Kind: IdentityLogical}},
		)
		require.NoError(t, err)
		require.NoError(t, handle.Close())
	}
	// Assert.
	rm := metrytest.CollectResourceMetrics(t, provider, metrics)
	hist := metrytest.FindFloat64Histogram(t, rm, metrytest.MetricSelector{Name: InvocationDurationMetricName})
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(t, uint64(1200), hist.DataPoints[0].Count)
	attrs := hist.DataPoints[0].Attributes
	assert.Equal(t, 3, attrs.Len())
	assert.Equal(t, "agent", testutil.SpanStringAttr(t, attrs, InvocationKindKey))
	assert.Equal(t, "local", testutil.SpanStringAttr(t, attrs, ExecutionLocalityKey))
	assert.Equal(t, OperationStatusUnset, testutil.SpanStringAttr(t, attrs, OperationStatus))
}

func TestInvocationAncestorsDoNotDuplicateAttemptResources(t *testing.T) {
	// Arrange.
	tracker, provider, metrics, _ := newTestTrackerWithMetrics(t, WithRawPayloads())
	ctx, workflow, err := tracker.StartWorkflow(
		context.Background(),
		WorkflowInvocation{Name: "pipeline"},
		RecordingOptions{},
	)
	require.NoError(t, err)
	ctx, agent, err := tracker.StartAgent(
		ctx,
		AgentInvocation{Name: "agent", Locality: ExecutionLocal},
		RecordingOptions{},
	)
	require.NoError(t, err)
	_, model, err := tracker.Recorder().
		ForProvider("openai").
		StartOperation(ctx, Operation{Name: "chat", Model: "model", Locality: ExecutionRemote}, RecordingOptions{Identity: CausalIdentity{RunID: "run", OperationID: "model-op", AttemptID: "attempt", Kind: IdentityAttempt}})
	require.NoError(t, err)
	// Act.
	require.NoError(
		t,
		model.Finish(
			OperationResult{
				Status: OperationStatusOK,
				Usage: Usage{
					InputTokens:  Observe(11, UnitTokens, SourceProvider),
					OutputTokens: Observe(7, UnitTokens, SourceProvider),
					Cost:         Observe(0.2, UnitCurrency, SourceProvider),
					Currency:     "USD",
				},
			},
		),
	)
	require.NoError(t, agent.Finish(InvocationResult{Status: OperationStatusOK}))
	require.NoError(t, workflow.Finish(InvocationResult{Status: OperationStatusOK}))
	// Assert: caller resource totals appear exactly once despite two ancestors.
	rm := metrytest.CollectResourceMetrics(t, provider, metrics)
	input := metrytest.FindInt64Histogram(t, rm, metrytest.MetricSelector{Name: InputTokenUsageMetricName})
	output := metrytest.FindInt64Histogram(t, rm, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	require.Len(t, input.DataPoints, 1)
	require.Len(t, output.DataPoints, 1)
	assert.Equal(t, uint64(1), input.DataPoints[0].Count)
	assert.Equal(t, int64(11), input.DataPoints[0].Sum)
	assert.Equal(t, uint64(1), output.DataPoints[0].Count)
	assert.Equal(t, int64(7), output.DataPoints[0].Sum)
	cost := metrytest.FindFloat64Sum(t, rm, metrytest.MetricSelector{Name: CostMetricName})
	require.Len(t, cost.DataPoints, 1)
	assert.InDelta(t, 0.2, cost.DataPoints[0].Value, 1e-12)
}
