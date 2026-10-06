package propagation_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	otelprop "go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/enrichment"
	prop "github.com/skosovsky/metry/propagation"
)

func TestOrdinaryW3CBaggageAdmittedWithLocalPrecedence(t *testing.T) {
	// Arrange: a type marker is optional only for strings, never evidence of trust.
	policy, err := prop.NewPolicy(prop.PolicyConfig{
		IngressBaggageKeys: []string{"value", "tenant", "msg", "host", "invalid", "empty"},
		ReservedKeys:       []string{"host"},
	})
	require.NoError(t, err)
	wrapped := prop.Wrap(otelprop.Baggage{}, policy)
	ctx := metry.Enrich(context.Background(), metry.StringAttribute("tenant", "local"))
	// Act.
	result := wrapped.Extract(ctx, otelprop.MapCarrier{
		"baggage": "value=hello%20world,tenant=remote,msg=forged,host=forged,private=secret,invalid=value;metry.attr.type=object,empty=value;metry.attr.type=",
	})
	// Assert.
	require.Equal(t, "hello world", enrichment.String(result, "value"))
	require.Equal(t, "local", enrichment.String(result, "tenant"))
	for _, key := range []string{"msg", "host", "private", "invalid", "empty"} {
		require.Empty(t, enrichment.String(result, key))
		require.Empty(t, baggage.FromContext(result).Member(key).Value())
	}
}

func TestPropagationSortedRetentionAfterMergeAndOnEgress(t *testing.T) {
	// Arrange: remote input fits SDK limits, merging with local provenance does not.
	keys := make([]string, 0, 70)
	attrs := make([]metry.Attribute, 0, 70)
	remote := make([]string, 0, 30)
	for index := range 70 {
		key := fmt.Sprintf("k%03d", index)
		keys = append(keys, key)
		attrs = append(attrs, metry.StringAttribute(key, "local-sentinel"))
		if index >= 40 {
			remote = append(remote, key+"=remote-sentinel")
		}
	}
	policy, err := prop.NewPolicy(prop.PolicyConfig{IngressBaggageKeys: keys, EgressBaggageKeys: keys})
	require.NoError(t, err)
	wrapped := prop.Wrap(otelprop.Baggage{}, policy)
	exporter := tracetest.NewInMemoryExporter()
	sdk := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { require.NoError(t, sdk.Shutdown(context.Background())) })
	ctx, span := sdk.Tracer("bounds").Start(context.Background(), "transport")
	ctx = metry.Enrich(ctx, attrs[:40]...)
	// Act: ingress merge overflows by six, egress uses full trusted values.
	result := wrapped.Extract(ctx, otelprop.MapCarrier{"baggage": strings.Join(remote, ",")})
	carrier := otelprop.MapCarrier{}
	for range 10 {
		wrapped.Inject(result, carrier)
		bag, parseErr := baggage.Parse(carrier.Get("baggage"))
		require.NoError(t, parseErr)
		// Assert: exact selected keys are stable despite SDK's map iteration order.
		require.Equal(t, 64, bag.Len())
		require.Equal(t, "local-sentinel", bag.Member("k000").Value())
		require.Equal(t, "remote-sentinel", bag.Member("k063").Value())
		require.Empty(t, bag.Member("k064").Value())
		require.LessOrEqual(t, len(carrier.Get("baggage")), 8192)
	}
	require.Equal(t, 64, baggage.FromContext(result).Len())
	require.Len(t, enrichment.Values(result), 70)
	require.Equal(t, "remote-sentinel", enrichment.String(result, "k069"))
	span.End()
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 11)
	for _, event := range spans[0].Events {
		require.Equal(t, "metry.baggage.dropped", event.Name)
		require.Len(t, event.Attributes, 2)
		for _, attr := range event.Attributes {
			require.NotContains(t, attr.Value.String(), "sentinel")
			if attr.Key == "metry.baggage.dropped_count" {
				require.Equal(t, int64(6), attr.Value.AsInt64())
			}
		}
	}
	// A local value beyond the visible limit can still be selected by an egress allowlist.
	onlyTail, err := prop.NewPolicy(prop.PolicyConfig{EgressBaggageKeys: []string{"k069"}})
	require.NoError(t, err)
	prop.Wrap(otelprop.Baggage{}, onlyTail).Inject(result, carrier)
	bag, err := baggage.Parse(carrier.Get("baggage"))
	require.NoError(t, err)
	require.Equal(t, "remote-sentinel", bag.Member("k069").Value())
	require.Equal(t, 1, bag.Len())
}

func TestPropagationEncodedByteRetention(t *testing.T) {
	// Arrange: local raw string lengths fit, percent-encoded wire lengths do not.
	policy, err := prop.NewPolicy(prop.PolicyConfig{EgressBaggageKeys: []string{"a", "b", "c"}})
	require.NoError(t, err)
	ctx := metry.Enrich(context.Background(),
		metry.StringAttribute("c", "small"), metry.StringAttribute("a", "small"),
		metry.StringAttribute("b", strings.Repeat("%", 3000)))
	wrapped := prop.Wrap(otelprop.Baggage{}, policy)
	// Act.
	carrier := otelprop.MapCarrier{}
	wrapped.Inject(ctx, carrier)
	// Assert: stop at b rather than skipping it to retain c.
	bag, err := baggage.Parse(carrier.Get("baggage"))
	require.NoError(t, err)
	require.Equal(t, 1, bag.Len())
	require.Equal(t, "small", bag.Member("a").Value())
	require.Empty(t, bag.Member("c").Value())
}
