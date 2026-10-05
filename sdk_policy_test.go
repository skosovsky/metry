package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/diagnostic"
	metryprop "github.com/skosovsky/metry/propagation"
)

func TestProviderPolicyOwnedAndBorrowedIsolation(t *testing.T) {
	// Arrange: policy and diagnostics belong to each provider, not the host SDK.
	policy, err := metryprop.NewPolicy(
		metryprop.PolicyConfig{IngressBaggageKeys: []string{"subject_id"}, EgressBaggageKeys: []string{"subject_id"}},
	)
	require.NoError(t, err)
	raw, err := diagnostic.NewPolicy(diagnostic.Config{Raw: true})
	require.NoError(t, err)
	sdk := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = sdk.Shutdown(context.Background()) })
	deps := SDKDependencies{
		TracerProvider: sdk,
		MeterProvider:  noop.NewMeterProvider(),
		Propagator:     propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	}
	allowed, err := FromSDK(deps, WithSDKPropagationPolicy(policy), WithSDKDiagnosticPolicy(raw))
	require.NoError(t, err)
	safe, err := FromSDK(deps)
	require.NoError(t, err)
	owned, err := New(
		context.Background(),
		WithServiceName("policy"),
		WithPropagationPolicy(policy),
		WithDiagnosticPolicy(raw),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = owned.Shutdown(context.Background()) })
	ctx := Enrich(context.Background(), SubjectID("local"), TenantID("not-allowed"))
	// Act: each transport sees only its own configured baggage.
	for _, provider := range []*Provider{allowed, owned} {
		carrier := map[string]any{}
		provider.InjectToMap(ctx, carrier)
		received := provider.SDK().Propagator.Extract(
			context.Background(),
			propagation.MapCarrier{"baggage": carrier["baggage"].(string)},
		)
		// Assert.
		require.Equal(t, "local", baggage.FromContext(received).Member("subject_id").Value())
		require.Empty(t, baggage.FromContext(received).Member("tenant_id").Value())
		text, textErr := provider.DiagnosticPolicy().Text(diagnostic.ChannelSecurityReason, "explicit raw")
		require.NoError(t, textErr)
		require.Equal(t, "explicit raw", text)
	}
	empty := map[string]any{}
	safe.InjectToMap(ctx, empty)
	require.NotContains(t, empty, "baggage")
	input := propagation.MapCarrier{"baggage": "subject_id=remote;metry.attr.type=string"}
	require.Empty(t, baggage.FromContext(safe.SDK().Propagator.Extract(context.Background(), input)).Members())
	text, textErr := safe.DiagnosticPolicy().Text(diagnostic.ChannelSecurityReason, "secret")
	require.NoError(t, textErr)
	require.NotContains(t, text, "secret")
	require.Equal(t, "subject_id=remote;metry.attr.type=string", input["baggage"])
}

func TestBorrowedRejectTraceAndNilOption(t *testing.T) {
	// Arrange.
	sdk := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = sdk.Shutdown(context.Background()) })
	deps := SDKDependencies{
		TracerProvider: sdk,
		MeterProvider:  noop.NewMeterProvider(),
		Propagator:     propagation.TraceContext{},
	}
	reject, err := metryprop.NewPolicy(metryprop.PolicyConfig{RejectRemoteTrace: true})
	require.NoError(t, err)
	provider, err := FromSDK(deps, WithSDKPropagationPolicy(reject))
	require.NoError(t, err)
	parent := trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled},
	)
	ctx := trace.ContextWithSpanContext(context.Background(), parent)
	// Act.
	result := provider.SDK().Propagator.Extract(
		ctx,
		propagation.MapCarrier{"traceparent": "00-11111111111111111111111111111111-2222222222222222-01"},
	)
	_, invalidErr := FromSDK(deps, nil)
	// Assert: rejection retains local parent and never takes SDK ownership.
	require.Equal(t, parent, trace.SpanContextFromContext(result))
	require.ErrorIs(t, invalidErr, ErrInvalidConfiguration)
	require.NoError(t, provider.Shutdown(context.Background()))
}
