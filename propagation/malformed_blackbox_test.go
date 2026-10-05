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

func TestMapMalformedWireDoesNotImportBusinessValues(t *testing.T) {
	for _, test := range []struct {
		name          string
		carrier       map[string]any
		traceAccepted bool
	}{
		{name: "invalid traceparent", carrier: map[string]any{"traceparent": "SECRET-invalid-parent"}},
		{name: "zero traceparent", carrier: map[string]any{"traceparent": "00-00000000000000000000000000000000-0000000000000000-01"}},
		{name: "malformed baggage", carrier: map[string]any{"baggage": "count=%ZZ;metry.attr.type=int"}},
		{name: "invalid int", carrier: map[string]any{"baggage": "count=SECRET;metry.attr.type=int"}},
		{name: "int overflow", carrier: map[string]any{"baggage": "count=9223372036854775808;metry.attr.type=int"}},
		{name: "invalid bool", carrier: map[string]any{"baggage": "passed=SECRET;metry.attr.type=bool"}},
		{name: "NaN", carrier: map[string]any{"baggage": "score=NaN;metry.attr.type=float"}},
		{name: "Inf", carrier: map[string]any{"baggage": "score=Inf;metry.attr.type=float"}},
		{name: "negative Inf", carrier: map[string]any{"baggage": "score=-Inf;metry.attr.type=float"}},
		{name: "unknown marker", carrier: map[string]any{"baggage": "metry.genai.model=SECRET;metry.attr.type=object"}},
		{name: "missing marker", carrier: map[string]any{"baggage": "metry.genai.model=SECRET"}},
		{name: "non-string fields", carrier: map[string]any{"traceparent": 123, "baggage": 456}},
		{name: "empty fields", carrier: map[string]any{"traceparent": "", "baggage": ""}},
		{name: "empty map", carrier: map[string]any{}},
		{name: "nil map", carrier: nil},
		{name: "valid parent malformed baggage", carrier: map[string]any{
			"traceparent": "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
			"baggage":     "count=%ZZ;metry.attr.type=int",
		}, traceAccepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange: explicitly allowing keys never makes malformed scalar content trusted.
			policy := trustPolicy(t, "count", "passed", "score", metry.GenAIBaggageModelKey)
			wrapped := prop.Wrap(
				otelprop.NewCompositeTextMapPropagator(otelprop.TraceContext{}, otelprop.Baggage{}),
				policy,
			)
			// Act: inspect imported context and actual slog output independently of wire inputs.
			imported := prop.ExtractFromMap(context.Background(), wrapped, test.carrier)
			scope, found := genai.ScopeFromContext(imported)
			var logs bytes.Buffer
			slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)}).
				InfoContext(imported, "host-message")
			// Assert.
			require.Equal(t, test.traceAccepted, trace.SpanContextFromContext(imported).IsValid())
			require.Empty(t, baggage.FromContext(imported).Members())
			require.False(t, found)
			require.Empty(t, scope.Model)
			require.NotContains(t, logs.String(), "SECRET")
			for _, key := range []string{"count", "passed", "score", metry.GenAIBaggageModelKey} {
				require.NotContains(t, logs.String(), `"`+key+`":`)
			}
			require.Contains(t, logs.String(), `"msg":"host-message"`)
		})
	}
}

// nilWirePropagator would panic if a typed nil boundary invoked its callbacks.
type nilWirePropagator struct{}

func (*nilWirePropagator) Fields() []string { panic("nil propagator callback") }
func (*nilWirePropagator) Inject(context.Context, otelprop.TextMapCarrier) {
	panic("nil propagator callback")
}
func (*nilWirePropagator) Extract(context.Context, otelprop.TextMapCarrier) context.Context {
	panic("nil propagator callback")
}

func TestMapNilPropagatorsDoNotImportWireEvidence(t *testing.T) {
	var typedNil *nilWirePropagator
	for _, boundary := range []otelprop.TextMapPropagator{nil, typedNil, prop.Wrap(typedNil, prop.Policy{})} {
		// Arrange.
		ctx := context.Background()
		carrier := map[string]any{
			"traceparent": "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
			"baggage":     "metry.genai.model=SECRET;metry.attr.type=string",
		}
		// Act.
		imported := prop.ExtractFromMap(ctx, boundary, carrier)
		prop.InjectToMap(imported, boundary, nil)
		_, found := genai.ScopeFromContext(imported)
		var logs bytes.Buffer
		slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)}).InfoContext(imported, "host-message")
		// Assert.
		require.False(t, trace.SpanContextFromContext(imported).IsValid())
		require.Empty(t, baggage.FromContext(imported).Members())
		require.False(t, found)
		require.NotContains(t, logs.String(), "SECRET")
		require.NotContains(t, logs.String(), metry.GenAIBaggageModelKey)
	}
}
