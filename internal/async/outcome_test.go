package async

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/testutil"
)

func TestStartLinkedSpan_CreatesLinkWithoutParent(t *testing.T) {
	mem := testutil.NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem.SDKSpanExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	linkedSC := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
		SpanID:  trace.SpanID{1, 1, 1, 1, 1, 1, 1, 1},
	})
	handle, err := newHandleFromSpanContext(linkedSC)
	require.NoError(t, err)

	_, span, err := StartLinkedSpan(context.Background(), tp.Tracer("t"), handle, "outcome")
	require.NoError(t, err)
	span.End()

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "outcome", spans[0].Name)
	assert.False(t, spans[0].Parent.SpanID().IsValid())
	require.NotEmpty(t, spans[0].Links)
}

func TestStartLinkedSpan_NilTracer_ReturnsErrNilTracer(t *testing.T) {
	handle, err := newHandleFromSpanContext(trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		SpanID:  trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2},
	}))
	require.NoError(t, err)

	_, _, err = StartLinkedSpan(context.Background(), nil, handle, "x")
	require.ErrorIs(t, err, ErrNilTracer)
}

func TestRecordLinkedOutcome_NilTracer_ReturnsErrNilTracer(t *testing.T) {
	handle, err := newHandleFromSpanContext(trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3},
		SpanID:  trace.SpanID{4, 4, 4, 4, 4, 4, 4, 4},
	}))
	require.NoError(t, err)

	err = handle.RecordLinkedOutcome(context.Background(), nil, "x", nil)
	require.ErrorIs(t, err, ErrNilTracer)
}
