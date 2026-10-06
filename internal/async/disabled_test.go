package async

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/testutil"
)

func TestDisabledHandleDropsInternalLinkedPaths(t *testing.T) {
	// Arrange.
	exports := testutil.NewInMemoryTraceExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exports.SDKSpanExporter()))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	handle := NewNoopHandle()
	ctx := t.Context()
	called := false
	// Act.
	restored, span, err := StartLinkedSpan(ctx, provider.Tracer("test"), handle, "disabled")
	require.NoError(t, err)
	span.End()
	callbackErr := handle.RunLinkedSpan(ctx, provider.Tracer("test"), "disabled", func(trace.Span) error {
		called = true
		return nil
	})
	outcomeErr := handle.RecordLinkedOutcome(ctx, provider.Tracer("test"), "disabled", nil)
	// Assert.
	require.Same(t, ctx, restored)
	require.True(t, handle.IsDisabled())
	require.False(t, span.SpanContext().IsValid())
	require.NoError(t, callbackErr)
	require.NoError(t, outcomeErr)
	require.False(t, called)
	require.Empty(t, exports.GetSpans())
}
