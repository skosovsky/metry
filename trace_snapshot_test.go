package metry

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	otelprop "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/skosovsky/metry/internal/enrichment"
	metryprop "github.com/skosovsky/metry/propagation"
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

type snapshotTransport struct{}

func (snapshotTransport) Fields() []string { return []string{"custom-trace"} }
func (snapshotTransport) Inject(context.Context, otelprop.TextMapCarrier) {
	panic("snapshot must not invoke transport injection")
}
func (snapshotTransport) Extract(context.Context, otelprop.TextMapCarrier) context.Context {
	panic("snapshot must not invoke transport extraction")
}

func snapshotTestReference(t *testing.T, last byte) trace.SpanContext {
	t.Helper()
	state, err := trace.ParseTraceState("vendor=value")
	require.NoError(t, err)
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3, last}, SpanID: trace.SpanID{4, 5, 6, last},
		TraceFlags: trace.FlagsSampled, TraceState: state, Remote: false,
	})
}

func TestTraceSnapshotRestoreIndependentOfTransport(t *testing.T) {
	for name, transport := range map[string]otelprop.TextMapPropagator{
		"w3c": otelprop.TraceContext{}, "baggage": otelprop.Baggage{},
		"custom": snapshotTransport{},
	} {
		for _, existing := range []bool{false, true} {
			suffix := "/empty"
			if existing {
				suffix = "/foreign"
			}
			t.Run(name+suffix, func(t *testing.T) {
				// Arrange: captured trace differs from an existing foreign trace.
				reference := snapshotTestReference(t, 7)
				snapshot, err := TraceSnapshotFromContext(trace.ContextWithSpanContext(context.Background(), reference))
				require.NoError(t, err)
				provider, err := FromSDK(SDKDependencies{
					TracerProvider: tracenoop.NewTracerProvider(),
					MeterProvider:  noop.NewMeterProvider(),
					Propagator:     transport,
				})
				require.NoError(t, err)
				ctx := Enrich(context.Background(), TenantID("caller-tenant"))
				if existing {
					ctx = trace.ContextWithSpanContext(ctx, snapshotTestReference(t, 8))
				}
				// Act.
				restored, err := provider.ContextWithTraceSnapshot(ctx, snapshot)
				// Assert: exact captured reference and caller context values survive.
				require.NoError(t, err)
				actual := trace.SpanContextFromContext(restored)
				require.True(t, actual.Equal(reference.WithRemote(true)))
				require.Equal(t, "caller-tenant", BaggageMember(restored, "tenant_id"))
				require.Equal(t, "caller-tenant", enrichment.String(restored, "tenant_id"))
			})
		}
	}
}

func TestTraceSnapshotPolicyRejectionPreservesContext(t *testing.T) {
	// Arrange: both owned and borrowed constructors must retain the same policy.
	policy, err := metryprop.NewPolicy(metryprop.PolicyConfig{RejectRemoteTrace: true})
	require.NoError(t, err)
	owned, _ := newTestProvider(t, WithPropagationPolicy(policy))
	borrowed, err := FromSDK(SDKDependencies{
		TracerProvider: tracenoop.NewTracerProvider(), MeterProvider: noop.NewMeterProvider(),
		Propagator: otelprop.Baggage{},
	}, WithSDKPropagationPolicy(policy))
	require.NoError(t, err)
	reference := snapshotTestReference(t, 7)
	snapshot, err := TraceSnapshotFromContext(trace.ContextWithSpanContext(context.Background(), reference))
	require.NoError(t, err)
	for _, provider := range []*Provider{owned, borrowed} {
		for _, existing := range []bool{false, true} {
			ctx := Enrich(context.Background(), TenantID("original"))
			if existing {
				ctx = trace.ContextWithSpanContext(ctx, snapshotTestReference(t, 8))
			}
			// Act.
			restored, err := provider.ContextWithTraceSnapshot(ctx, snapshot)
			// Assert: no successful noop and no replacement of existing trace/value.
			require.ErrorIs(t, err, ErrTraceSnapshotRejected)
			require.Same(t, ctx, restored)
			require.True(t, trace.SpanContextFromContext(restored).Equal(trace.SpanContextFromContext(ctx)))
			require.Equal(t, "original", BaggageMember(restored, "tenant_id"))
		}
	}
}

func TestTraceSnapshotNilAndZeroProviderRestore(t *testing.T) {
	// Arrange.
	reference := snapshotTestReference(t, 7)
	snapshot, err := TraceSnapshotFromContext(trace.ContextWithSpanContext(context.Background(), reference))
	require.NoError(t, err)
	for _, provider := range []*Provider{nil, {}} {
		// Act.
		restored, err := provider.ContextWithTraceSnapshot(context.Background(), snapshot)
		// Assert.
		require.NoError(t, err)
		require.True(t, trace.SpanContextFromContext(restored).Equal(reference.WithRemote(true)))
	}
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
