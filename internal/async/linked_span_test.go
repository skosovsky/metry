package async

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/testutil"
)

func TestRunLinkedSpan_CallbackError_SetsSpanStatus(t *testing.T) {
	mem := testutil.NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem.SDKSpanExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	linkedSC := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
		SpanID:  trace.SpanID{1, 1, 1, 1, 1, 1, 1, 1},
	})
	handle, err := newHandleFromSpanContext(linkedSC)
	require.NoError(t, err)

	wantErr := errors.New("boom")
	err = handle.RunLinkedSpan(context.Background(), tp.Tracer("t"), "eval.result", func(trace.Span) error {
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status.Code)
}

func TestRunLinkedSpan_Success_SetsOkStatus(t *testing.T) {
	mem := testutil.NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem.SDKSpanExporter()))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	linkedSC := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
		SpanID:  trace.SpanID{1, 1, 1, 1, 1, 1, 1, 1},
	})
	handle, err := newHandleFromSpanContext(linkedSC)
	require.NoError(t, err)

	err = handle.RunLinkedSpan(context.Background(), tp.Tracer("t"), "eval.result", func(trace.Span) error {
		return nil
	})
	require.NoError(t, err)

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Ok, spans[0].Status.Code)
}

type unformattedPanic struct{ calls *int }

func (p *unformattedPanic) String() string { *p.calls++; return "SENTINEL_SECRET" }

func TestRunLinkedSpan_PanicSafeDefault(t *testing.T) {
	mem := testutil.NewInMemoryTraceExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem.SDKSpanExporter()))
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })
	calls := 0
	original := &unformattedPanic{&calls}
	func() {
		defer func() { require.Same(t, original, recover()) }()
		_ = NewNoopHandle().RunLinkedSpan(context.Background(), tp.Tracer("test"), "linked", func(trace.Span) error { panic(original) })
	}()
	require.Zero(t, calls)
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "panic", spans[0].Status.Description)
	require.Len(t, spans[0].Events, 1)
}
