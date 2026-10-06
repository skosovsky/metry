package metry

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"

	"github.com/skosovsky/metry/diagnostic"
	"github.com/skosovsky/metry/testutil"
)

func TestDisabledAsyncHandleCannotRepresentOrExportAnOrigin(t *testing.T) {
	// Arrange.
	provider, exports := newTestProvider(t)
	handle := NoopAsyncHandle()
	called := false
	// Act.
	token, err := handle.Marshal()
	spanErr := handle.RecordLinkedSpan(context.Background(), provider, "disabled", func(LinkedSpanWriter) error {
		called = true
		return errors.New("must not run")
	})
	outcomeErr := handle.RecordLinkedOutcomeWithProvider(context.Background(), provider, "disabled")
	require.NoError(t, provider.ForceFlush(context.Background()))
	// Assert: explicit disabled differs from invalid, and is not serializable.
	require.False(t, handle.IsValid())
	require.False(t, handle.h.SpanContext().IsValid())
	require.ErrorIs(t, err, ErrInvalidAsyncHandle)
	require.Empty(t, token)
	require.NoError(t, spanErr)
	require.NoError(t, outcomeErr)
	require.False(t, called)
	require.Empty(t, exports.GetSpans())
	require.NoError(t, handle.RecordLinkedSpan(context.Background(), nil, "disabled", nil))
	require.NoError(t, handle.RecordLinkedOutcomeWithProvider(context.Background(), nil, "disabled"))
	require.ErrorIs(t,
		(AsyncHandle{}).RecordLinkedSpan(context.Background(), provider, "invalid", nil), ErrInvalidAsyncHandle)
}

func TestLinkedWriterRetainsFirstErrorAndCallbackFailureTakesPrecedence(t *testing.T) {
	for _, callbackFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "writer error", true: "callback error"}[callbackFailure], func(t *testing.T) {
			// Arrange.
			policy, err := diagnostic.NewPolicy(diagnostic.Config{Raw: true})
			require.NoError(t, err)
			provider, exports := newTestProvider(t, WithDiagnosticPolicy(policy))
			ctx, end, err := provider.StartSpan(context.Background(), "test", "origin")
			require.NoError(t, err)
			handle, err := NewAsyncHandle(ctx)
			require.NoError(t, err)
			end()
			first, later, returned := errors.New("first"), errors.New("later"), errors.New("returned")
			var escaped LinkedSpanWriter
			// Act.
			callback := func(writer LinkedSpanWriter) error {
				escaped = writer
				writer.SetError(nil)
				writer.SetError(first)
				writer.SetError(later)
				writer.SetError(nil)
				writer.SetAttributes(StringAttribute("during", "kept"))
				writer.AddEvent("during")
				if callbackFailure {
					return returned
				}
				return nil
			}
			err = handle.RecordLinkedSpan(context.Background(), provider, "linked", callback)
			escaped.SetError(later)
			escaped.SetAttributes(StringAttribute("after", "dropped"))
			escaped.AddEvent("after")
			require.NoError(t, provider.ForceFlush(context.Background()))
			// Assert.
			want := first
			if callbackFailure {
				require.Same(t, returned, err)
				want = returned
			} else {
				require.NoError(t, err)
			}
			spans := exports.GetSpans()
			require.Len(t, spans, 2)
			span := spans[1]
			require.Equal(t, codes.Error, span.Status.Code)
			require.Equal(t, want.Error(), span.Status.Description)
			require.Equal(t, "kept", testutil.SpanStubStringAttr(t, span, "during"))
			require.False(t, testutil.SpanStubHasAttr(span, "after"))
			for _, event := range span.Events {
				require.NotEqual(t, "after", event.Name)
			}
		})
	}
}

func TestLinkedWriterConcurrentWorkersJoinBeforeReturn(t *testing.T) {
	// Arrange.
	provider, exports := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "origin")
	require.NoError(t, err)
	handle, err := NewAsyncHandle(ctx)
	require.NoError(t, err)
	end()
	first := errors.New("first")
	const workers = 32
	// Act.
	err = handle.RecordLinkedSpan(context.Background(), provider, "concurrent", func(writer LinkedSpanWriter) error {
		writer.SetError(first)
		var joined sync.WaitGroup
		for i := range workers {
			joined.Go(func() {
				writer.SetAttributes(IntAttribute("worker", int64(i)))
				writer.AddEvent("worker", IntAttribute("index", int64(i)))
				writer.SetError(errors.New("later"))
				writer.SetError(nil)
			})
		}
		joined.Wait()
		return nil
	})
	require.NoError(t, provider.ForceFlush(context.Background()))
	// Assert.
	require.NoError(t, err)
	spans := exports.GetSpans()
	require.Len(t, spans, 2)
	require.Equal(t, codes.Error, spans[1].Status.Code)
	count := 0
	for _, event := range spans[1].Events {
		if event.Name == "worker" {
			count++
		}
	}
	require.Equal(t, workers, count)
	// A zero writer is safe for optional callback composition.
	var zero LinkedSpanWriter
	zero.SetError(first)
	zero.SetError(nil)
	zero.SetAttributes(TenantID("tenant"))
	zero.AddEvent("ignored")
}
