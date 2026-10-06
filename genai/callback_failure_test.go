package genai

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/diagnostic"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

const callbackSecret = "host-callback-secret-never-export"

type panickingPayloadPolicy struct {
	method string
	value  any
}

func (p panickingPayloadPolicy) SanitizePayload(v Payload) Payload {
	if p.method == "payload" {
		panic(p.value)
	}
	return v
}
func (p panickingPayloadPolicy) SanitizeToolCall(v ToolCall) ToolCall {
	if p.method == "call" {
		panic(p.value)
	}
	return v
}
func (p panickingPayloadPolicy) SanitizeToolResult(v ToolResult) ToolResult {
	if p.method == "result" {
		panic(p.value)
	}
	return v
}

func newCallbackFailureTracker(
	t *testing.T,
	method string,
	value any,
) (*Tracker, *metry.Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	raw, err := diagnostic.NewPolicy(diagnostic.Config{Raw: true})
	require.NoError(t, err)
	provider, exports := metrytest.NewTestProvider(t, metry.WithDiagnosticPolicy(raw))
	tracker, err := NewTrackerFromProvider(provider, WithProfile(testInstrumentationProfile()),
		WithPayloadPolicy(panickingPayloadPolicy{method: method, value: value}),
		WithToolErrorClassifier(ToolErrorClassifierFunc(func(error) ErrorClass { panic(value) })))
	require.NoError(t, err)
	return tracker, provider, exports
}

func TestHostCallbackPanicFinalizesOwnedIntervals(t *testing.T) {
	type recording struct {
		ctx    context.Context
		finish func() error
		close  func() error
	}
	cases := map[string]struct {
		method string
		start  func(*testing.T, *Tracker) recording
	}{
		"tool payload": {"result", func(t *testing.T, tr *Tracker) recording {
			ctx, h, err := tr.Recorder().StartTool(t.Context(), ToolCall{Name: "search"})
			require.NoError(t, err)
			return recording{
				ctx,
				func() error { return h.Finish(ToolResult{Status: OperationStatusOK, Result: `{"value":"safe"}`}) },
				h.Close,
			}
		}},
		"tool classifier": {"", func(t *testing.T, tr *Tracker) recording {
			ctx, h, err := tr.Recorder().StartTool(t.Context(), ToolCall{Name: "search"})
			require.NoError(t, err)
			return recording{
				ctx,
				func() error { return h.Finish(ToolResult{Err: errors.New(callbackSecret)}) },
				h.Close,
			}
		}},
		"operation payload": {"payload", func(t *testing.T, tr *Tracker) recording {
			ctx, h, err := tr.Recorder().
				StartOperation(t.Context(), Operation{Name: "chat", Locality: ExecutionRemote}, RecordingOptions{})
			require.NoError(t, err)
			return recording{
				ctx,
				func() error { return h.Finish(OperationResult{Status: OperationStatusOK}) },
				h.Close,
			}
		}},
		"retrieval classifier": {"", func(t *testing.T, tr *Tracker) recording {
			ctx, h, err := tr.StartRetrieval(
				t.Context(),
				"search",
				RetrievalRequest{Locality: ExecutionRemote},
			)
			require.NoError(t, err)
			return recording{
				ctx,
				func() error { return h.Finish(RetrievalResult{Err: errors.New(callbackSecret)}) },
				h.Close,
			}
		}},
		"agent classifier": {"", func(t *testing.T, tr *Tracker) recording {
			ctx, h, err := tr.StartAgent(
				t.Context(),
				AgentInvocation{Name: "agent", Locality: ExecutionLocal},
				RecordingOptions{},
			)
			require.NoError(t, err)
			return recording{
				ctx,
				func() error { return h.Finish(InvocationResult{Err: errors.New(callbackSecret)}) },
				h.Close,
			}
		}},
		"workflow classifier": {"", func(t *testing.T, tr *Tracker) recording {
			ctx, h, err := tr.StartWorkflow(
				t.Context(),
				WorkflowInvocation{Name: "workflow"},
				RecordingOptions{},
			)
			require.NoError(t, err)
			return recording{
				ctx,
				func() error { return h.Finish(InvocationResult{Err: errors.New(callbackSecret)}) },
				h.Close,
			}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange: raw diagnostics must still never receive the host panic object.
			sentinel := &struct{ Secret string }{callbackSecret}
			tracker, provider, exports := newCallbackFailureTracker(t, tc.method, sentinel)
			r := tc.start(t, tracker)
			// Act: host catches the original object after the span has ended.
			require.PanicsWithValue(t, sentinel, func() { _ = r.finish() })
			// Assert: failed telemetry cannot subsequently be completed as success.
			require.False(t, trace.SpanFromContext(r.ctx).IsRecording())
			require.ErrorIs(t, r.close(), ErrFinalized)
			require.ErrorIs(t, r.finish(), ErrFinalized)
			flushTestProvider(t, provider)
			spans := exports.GetSpans()
			require.Len(t, spans, 1)
			require.Equal(t, codes.Error, spans[0].Status.Code)
			require.Equal(t, string(ErrorClassUnknown), spans[0].Status.Description)
			require.NotContains(t, fmt.Sprint(spans), callbackSecret)
			attrs := map[string]string{}
			for _, a := range spans[0].Attributes {
				attrs[string(a.Key)] = a.Value.AsString()
			}
			require.Equal(t, OperationStatusError, attrs[OperationStatus])
			require.Equal(t, string(ErrorClassUnknown), attrs[ErrorType])
			found := false
			for _, e := range spans[0].Events {
				if e.Name == "metry.observation.rejected" {
					found = true
					require.Len(t, e.Attributes, 2)
					require.Equal(t, "metry.diagnostic.field", string(e.Attributes[0].Key))
					require.Equal(t, "callback", e.Attributes[0].Value.AsString())
					require.Equal(t, "metry.diagnostic.rule", string(e.Attributes[1].Key))
					require.Equal(t, "panic", e.Attributes[1].Value.AsString())
				}
			}
			require.True(t, found)
		})
	}
}

func TestHostCallbackPanicBeforeStartExportsNothing(t *testing.T) {
	cases := map[string]struct {
		method string
		call   func(*testing.T, *Tracker)
	}{
		"tool start": {"call", func(t *testing.T, tr *Tracker) {
			_, _, _ = tr.Recorder().
				StartTool(t.Context(), ToolCall{Name: "search", Arguments: `{"value":1}`})
		}},
		"retrieval start": {"payload", func(t *testing.T, tr *Tracker) {
			_, _, _ = tr.StartRetrieval(
				t.Context(),
				"search",
				RetrievalRequest{Locality: ExecutionRemote, Query: "query"},
			)
		}},
		"completed interaction": {"payload", func(t *testing.T, tr *Tracker) {
			_ = tr.RecordInteraction(t.Context(), Meta{Operation: "chat"}, Payload{}, Usage{})
		}},
		"completed operation": {"payload", func(t *testing.T, tr *Tracker) {
			_ = tr.Recorder().
				RecordOperation(t.Context(), Operation{Name: "chat"}, OperationResult{})
		}},
		"stream completion": {"payload", func(t *testing.T, tr *Tracker) {
			_ = tr.RecordStreamingCompletion(
				t.Context(),
				StreamingCompletion{Meta: Meta{Operation: "chat"}, TotalDuration: time.Millisecond},
			)
		}},
		"evaluation": {"payload", func(t *testing.T, tr *Tracker) {
			e := successfulEvaluation()
			e.Reasoning = "reason"
			_ = tr.EvaluationRecorder().Record(t.Context(), e)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			sentinel := &struct{ Secret string }{callbackSecret}
			tr, provider, exports := newCallbackFailureTracker(t, tc.method, sentinel)
			// Act.
			require.PanicsWithValue(t, sentinel, func() { tc.call(t, tr) })
			flushTestProvider(t, provider)
			// Assert.
			require.Empty(t, exports.GetSpans())
		})
	}
}

func TestHostCallbackPanicPreservesBorrowedSpan(t *testing.T) {
	for _, kind := range []string{"trace IO", "feedback", "async classifier"} {
		t.Run(kind, func(t *testing.T) {
			// Arrange: the origin span belongs to the host, not the GenAI recorder.
			sentinel := &struct{ Secret string }{callbackSecret}
			tr, provider, exports := newCallbackFailureTracker(t, "payload", sentinel)
			ctx, span := provider.SDK().TracerProvider.Tracer("host").Start(t.Context(), "origin")
			handle, err := metry.NewAsyncHandle(ctx)
			require.NoError(t, err)
			// Act.
			require.PanicsWithValue(t, sentinel, func() {
				switch kind {
				case "trace IO":
					_ = tr.RecordTraceIO(ctx, Payload{}, Payload{})
				case "feedback":
					_ = tr.RecordAsyncFeedback(ctx, handle, 0, "feedback")
				case "async classifier":
					_ = tr.Recorder().
						RecordAsyncResult(ctx, handle, AsyncResult{Err: errors.New(callbackSecret)})
				}
			})
			// Assert: no child/root was started, and the host retains ownership.
			require.True(t, span.IsRecording())
			span.End()
			flushTestProvider(t, provider)
			spans := exports.GetSpans()
			require.Len(t, spans, 1)
			require.Equal(t, codes.Unset, spans[0].Status.Code)
			require.NotContains(t, fmt.Sprint(spans), callbackSecret)
		})
	}
}
