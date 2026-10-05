package propagation_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	otelprop "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	prop "github.com/skosovsky/metry/propagation"
)

func trustPolicy(t *testing.T, keys ...string) prop.Policy {
	t.Helper()
	p, err := prop.NewPolicy(prop.PolicyConfig{IngressBaggageKeys: keys, EgressBaggageKeys: keys})
	require.NoError(t, err)
	return p
}

func TestPolicyDefaultRejectsMarkerAndManualBaggage(t *testing.T) {
	// Arrange.
	base := otelprop.NewCompositeTextMapPropagator(otelprop.TraceContext{}, otelprop.Baggage{})
	wrapped := prop.Wrap(base, prop.Policy{})
	carrier := otelprop.MapCarrier{
		"baggage":     "tenant_id=SECRET;metry.attr.type=string,msg=forged;metry.attr.type=string,metry.genai.model=attacker;metry.attr.type=string",
		"traceparent": "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
	}
	// Act.
	ctx := wrapped.Extract(context.Background(), carrier)
	scope, found := genai.ScopeFromContext(ctx)
	var buf bytes.Buffer
	slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)}).InfoContext(ctx, "real")
	// Assert.
	require.True(t, trace.SpanContextFromContext(ctx).IsValid())
	require.False(t, found)
	require.Empty(t, scope.Model)
	require.NotContains(t, buf.String(), "SECRET")
	require.NotContains(t, buf.String(), "forged")
	bag, err := baggage.Parse(carrier["baggage"])
	require.NoError(t, err)
	manual := baggage.ContextWithBaggage(context.Background(), bag)
	_, found = genai.ScopeFromContext(manual)
	require.False(t, found)
	buf.Reset()
	slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)}).InfoContext(manual, "real")
	require.NotContains(t, buf.String(), "SECRET")
}

func TestPolicyLocalWinsReservedAndHandlerAttrs(t *testing.T) {
	// Arrange.
	keys := []string{"tenant_id", "score", "passed", "count", "msg", "host", "metry.genai.model"}
	config := prop.PolicyConfig{IngressBaggageKeys: keys, EgressBaggageKeys: keys, ReservedKeys: []string{"host"}}
	p, err := prop.NewPolicy(config)
	require.NoError(t, err)
	keys[0] = "mutated"
	wrapped := prop.Wrap(otelprop.Baggage{}, p)
	local := metry.Enrich(context.Background(), metry.TenantID("local"))
	// Act.
	ctx := wrapped.Extract(
		local,
		otelprop.MapCarrier{
			"baggage": "tenant_id=remote;metry.attr.type=string,score=0.5;metry.attr.type=float,passed=true;metry.attr.type=bool,count=9;metry.attr.type=int,msg=forged;metry.attr.type=string,host=spoof;metry.attr.type=string,metry.genai.model=gpt;metry.attr.type=string",
		},
	)
	var buf bytes.Buffer
	logger := slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)}).With("score", 0.8)
	logger.InfoContext(ctx, "real", "count", 12)
	// Assert.
	require.Equal(t, "local", metry.BaggageMember(ctx, "tenant_id"))
	require.Equal(t, "local", metry.BaggageMember(local, "tenant_id"))
	require.Empty(t, metry.BaggageMember(local, "count"))
	require.Contains(t, buf.String(), `"passed":true`)
	require.Contains(t, buf.String(), `"count":12`)
	require.NotContains(t, buf.String(), `"count":9`)
	require.NotContains(t, buf.String(), "forged")
	require.NotContains(t, buf.String(), "spoof")
	require.NotContains(t, buf.String(), `"score":0.5`)
	scope, found := genai.ScopeFromContext(ctx)
	require.True(t, found)
	require.Equal(t, "gpt", scope.Model)
	out := otelprop.MapCarrier{}
	wrapped.Inject(ctx, out)
	round := wrapped.Extract(context.Background(), out)
	var roundBuf bytes.Buffer
	slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&roundBuf, nil)}).InfoContext(round, "round")
	require.Contains(t, roundBuf.String(), `"score":0.5`)
	require.Contains(t, roundBuf.String(), `"count":9`)
}

func TestPolicyDefaultEgressScopeCannotBypassAndCarrierClears(t *testing.T) {
	// Arrange.
	wrapped := prop.Wrap(
		otelprop.NewCompositeTextMapPropagator(otelprop.TraceContext{}, otelprop.Baggage{}),
		prop.Policy{},
	)
	ctx := genai.WithScope(context.Background(), genai.Scope{Model: "secret-model"})
	carrier := map[string]any{"traceparent": "old", "tracestate": "old", "baggage": "old", "host": 42}
	// Act.
	prop.InjectToMap(ctx, wrapped, carrier)
	// Assert.
	require.Equal(t, 42, carrier["host"])
	require.Empty(t, carrier["traceparent"])
	require.Empty(t, carrier["tracestate"])
	require.Empty(t, carrier["baggage"])
}
