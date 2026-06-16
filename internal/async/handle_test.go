package async

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestHandle_MarshalParse_RoundTrip(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("t").Start(context.Background(), "root")
	handle, err := NewHandle(ctx)
	require.NoError(t, err)
	span.End()

	token, err := handle.Marshal()
	require.NoError(t, err)

	parsed, err := ParseHandle(token)
	require.NoError(t, err)
	assert.True(t, parsed.IsValid())

	token2, err := parsed.Marshal()
	require.NoError(t, err)
	assert.Equal(t, token, token2)
}

func TestHandle_MarshalParse_PreservesTraceState(t *testing.T) {
	ts, err := trace.ParseTraceState("vendor=opaque")
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{8, 7, 6, 5, 4, 3, 2, 1},
		TraceFlags: trace.FlagsSampled,
		TraceState: ts,
		Remote:     true,
	})
	handle, err := newHandleFromSpanContext(sc)
	require.NoError(t, err)

	token, err := handle.Marshal()
	require.NoError(t, err)
	assert.Equal(t, "vendor=opaque", traceStateFromToken(t, token))
}

func traceStateFromToken(t *testing.T, token string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, err)
	var payload handlePayload
	require.NoError(t, json.Unmarshal(raw, &payload))
	return payload.TraceState
}

func TestParseHandle_EmptyToken_ReturnsError(t *testing.T) {
	_, err := ParseHandle("")
	require.ErrorIs(t, err, ErrInvalidHandle)
}

func TestParseHandle_OversizedToken_ReturnsError(t *testing.T) {
	_, err := ParseHandle(strings.Repeat("A", maxHandleTokenLen+1))
	require.ErrorIs(t, err, ErrHandleTokenTooLarge)
}

func TestParseHandle_MalformedToken_ReturnsError(t *testing.T) {
	_, err := ParseHandle("not-valid-base64!!!")
	require.ErrorIs(t, err, ErrInvalidHandle)
}

func TestMarshal_OversizedTraceState_ReturnsErrHandleTokenTooLarge(t *testing.T) {
	parts := []string{"vendor=opaque"}
	for len(parts) < 200 {
		parts = append(parts, "k"+strings.Repeat("x", len(parts))+"=v")
		ts, err := trace.ParseTraceState(strings.Join(parts, ","))
		require.NoError(t, err)
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
			SpanID:     trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2},
			TraceFlags: trace.FlagsSampled,
			TraceState: ts,
		})
		handle, err := newHandleFromSpanContext(sc)
		require.NoError(t, err)

		_, err = handle.Marshal()
		if errors.Is(err, ErrHandleTokenTooLarge) {
			return
		}
		require.NoError(t, err)
	}
	t.Fatal("expected marshal to exceed maxHandleTokenLen")
}

func TestNewHandle_NoSpan_ReturnsError(t *testing.T) {
	_, err := NewHandle(context.Background())
	require.ErrorIs(t, err, ErrNoSpanContext)
}
