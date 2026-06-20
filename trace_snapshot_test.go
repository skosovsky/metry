package metry

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceSnapshot_RoundTripRestoresTraceContinuation(t *testing.T) {
	provider, mem := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "root")
	require.NoError(t, err)
	snapshot, err := TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	token, err := snapshot.Marshal()
	require.NoError(t, err)
	end()

	parsed, err := ParseTraceSnapshot(token)
	require.NoError(t, err)
	childCtx, err := provider.ContextWithTraceSnapshot(context.Background(), parsed)
	require.NoError(t, err)
	_, childEnd, err := provider.StartSpan(childCtx, "test", "child")
	require.NoError(t, err)
	childEnd()

	require.NoError(t, provider.ForceFlush(context.Background()))
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
	assert.True(t, trace.SpanContextFromContext(childCtx).IsRemote())
}

func TestTraceSnapshot_InvalidTokenDoesNotMutateContext(t *testing.T) {
	parsed, err := ParseTraceSnapshot("not-base64")
	require.ErrorIs(t, err, ErrInvalidTraceSnapshot)

	ctx, restoreErr := (&Provider{}).ContextWithTraceSnapshot(context.Background(), parsed)

	require.ErrorIs(t, restoreErr, ErrInvalidTraceSnapshot)
	assert.False(t, trace.SpanContextFromContext(ctx).IsValid())
}

func TestTraceSnapshot_RejectsOversizedToken(t *testing.T) {
	_, err := ParseTraceSnapshot(strings.Repeat("x", maxTraceSnapshotTokenLen+1))
	require.ErrorIs(t, err, ErrTraceSnapshotTokenTooLarge)
}

func TestTraceSnapshot_DoesNotCaptureBaggage(t *testing.T) {
	provider, _ := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "root")
	require.NoError(t, err)
	ctx = Enrich(ctx, TenantID("tenant-secret"))
	snapshot, err := TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	end()

	resumed, err := provider.ContextWithTraceSnapshot(context.Background(), snapshot)
	require.NoError(t, err)

	assert.Empty(t, BaggageMember(resumed, "tenant_id"))
}

func TestTraceSnapshot_AllowsLongValidTraceState(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	require.NoError(t, err)
	traceState, err := trace.ParseTraceState(
		"a=" + strings.Repeat("a", 200) + ",b=" + strings.Repeat("b", 200),
	)
	require.NoError(t, err)
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceState: traceState,
	}))

	snapshot, err := TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	token, err := snapshot.Marshal()
	require.NoError(t, err)
	parsed, err := ParseTraceSnapshot(token)
	require.NoError(t, err)
	resumed, err := (&Provider{}).ContextWithTraceSnapshot(context.Background(), parsed)
	require.NoError(t, err)

	assert.Equal(t, traceState.String(), trace.SpanContextFromContext(resumed).TraceState().String())
}
