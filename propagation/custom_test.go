package propagation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	otelprop "go.opentelemetry.io/otel/propagation"
)

type customKey struct{}
type customPropagator struct{}

func (customPropagator) Fields() []string { return []string{"x-custom-trace"} }
func (customPropagator) Inject(ctx context.Context, carrier otelprop.TextMapCarrier) {
	if v, ok := ctx.Value(customKey{}).(string); ok {
		carrier.Set("x-custom-trace", v)
	}
}
func (customPropagator) Extract(ctx context.Context, carrier otelprop.TextMapCarrier) context.Context {
	if v := carrier.Get("x-custom-trace"); v != "" {
		return context.WithValue(ctx, customKey{}, v)
	}
	return ctx
}

func TestMapCustomDeclaredFieldsAndNil(t *testing.T) {
	// Arrange.
	prop := Wrap(customPropagator{}, Policy{})
	carrier := map[string]any{"x-custom-trace": "stale", "traceparent": "host", "job": 42}
	ctx := context.WithValue(context.Background(), customKey{}, "new")
	// Act.
	InjectToMap(ctx, prop, carrier)
	got := ExtractFromMap(context.Background(), prop, carrier)
	// Assert.
	require.Equal(t, "new", got.Value(customKey{}))
	require.Equal(t, "host", carrier["traceparent"])
	require.Equal(t, 42, carrier["job"])
	InjectToMap(context.Background(), prop, carrier)
	require.NotContains(t, carrier, "x-custom-trace")
	require.Equal(t, "host", carrier["traceparent"])
	require.Equal(t, ctx, ExtractFromMap(ctx, nil, carrier))
	InjectToMap(ctx, nil, carrier)
	InjectToMap(ctx, prop, nil)
	require.Equal(t, ctx, ExtractFromMap(ctx, prop, nil))
	require.Equal(t, ctx, ExtractFromMap(ctx, prop, map[string]any{"x-custom-trace": 123}))
	require.Equal(t, ctx, ExtractFromMap(ctx, prop, map[string]any{"x-custom-trace": ""}))
	require.Equal(t, ctx, Wrap(nil, Policy{}).Extract(ctx, nil))
}

func TestPolicyRejectsInvalidConfiguration(t *testing.T) {
	for _, config := range []PolicyConfig{
		{IngressBaggageKeys: []string{""}},
		{EgressBaggageKeys: []string{"bad key"}},
		{ReservedKeys: []string{" spaced"}},
	} {
		// Arrange / Act.
		_, err := NewPolicy(config)
		// Assert.
		require.Error(t, err)
		require.Equal(t, "invalid propagation policy key", err.Error())
	}
}

func TestPolicyTypedNilBoundaries(t *testing.T) {
	// Arrange.
	var carrier otelprop.MapCarrier
	var base *customPropagator
	ctx := context.Background()
	// Act / Assert: no callback or map writes on typed nil values.
	wrapped := Wrap(base, Policy{})
	wrapped.Inject(ctx, carrier)
	require.Equal(t, ctx, wrapped.Extract(ctx, carrier))
	InjectToMap(ctx, base, map[string]any{})
	require.Equal(t, ctx, ExtractFromMap(ctx, base, map[string]any{"x-custom-trace": "value"}))
}
