package metry_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
)

func TestContinuationTokens_MaxTraceStateRoundTrip(t *testing.T) {
	for _, character := range []string{"x", "<", ">", "&", "\"", "\\"} {
		t.Run(character, func(t *testing.T) {
			testMaxTraceStateRoundTrip(t, character)
		})
	}
}

func testMaxTraceStateRoundTrip(t *testing.T, character string) {
	t.Helper()
	// Arrange: two maximum-sized W3C members total exactly 512 serialized bytes.
	state, err := trace.ParseTraceState("a=" + strings.Repeat(character, 255) + ",b=" + strings.Repeat(character, 252))
	require.NoError(t, err)
	require.Len(t, state.String(), 512)
	origin := trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID:    trace.TraceID{1},
			SpanID:     trace.SpanID{2},
			TraceFlags: trace.FlagsSampled,
			TraceState: state,
			Remote:     true,
		},
	)
	ctx := trace.ContextWithSpanContext(context.Background(), origin)
	// Act.
	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	snapshotToken, err := snapshot.Marshal()
	require.NoError(t, err)
	restoredSnapshot, err := metry.ParseTraceSnapshot(snapshotToken)
	require.NoError(t, err)
	restoredCtx, err := (*metry.Provider)(nil).ContextWithTraceSnapshot(context.Background(), restoredSnapshot)
	require.NoError(t, err)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)
	reencoded, err := parsed.Marshal()
	require.NoError(t, err)
	// Assert.
	require.Equal(t, origin.TraceState().String(), trace.SpanContextFromContext(restoredCtx).TraceState().String())
	require.Equal(t, token, reencoded)
	require.LessOrEqual(t, len(token), 2048)
	require.LessOrEqual(t, len(snapshotToken), 2048)
}
