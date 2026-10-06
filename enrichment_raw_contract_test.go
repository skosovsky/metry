package metry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"

	"github.com/skosovsky/metry/internal/baggageutil"
	"github.com/skosovsky/metry/internal/enrichment"
	metryprop "github.com/skosovsky/metry/propagation"
)

func TestEnrichRawValuesAcrossSignalsAndTransport(t *testing.T) {
	for _, value := range []string{"simple", "hello world", "100%", "Косовский", "a,b", "x%20y"} {
		t.Run(value, func(t *testing.T) {
			// Arrange.
			policy, err := metryprop.NewPolicy(metryprop.PolicyConfig{
				IngressBaggageKeys: []string{"value"}, EgressBaggageKeys: []string{"value"},
			})
			require.NoError(t, err)
			provider, exports := newTestProvider(t, WithPropagationPolicy(policy))
			ctx, end, err := provider.StartSpan(context.Background(), "raw", "enrichment")
			require.NoError(t, err)
			var logs bytes.Buffer
			logger := slog.New(ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)})
			// Act.
			ctx = Enrich(ctx, StringAttribute("value", value))
			logger.InfoContext(ctx, "observation")
			carrier := map[string]any{}
			provider.InjectToMap(ctx, carrier)
			imported := provider.ExtractFromMap(context.Background(), carrier)
			end()
			require.NoError(t, provider.ForceFlush(context.Background()))
			// Assert: literal percent escapes are not interpreted as pre-encoded input.
			require.Equal(t, value, BaggageMember(ctx, "value"))
			require.Equal(t, value, enrichment.String(ctx, "value"))
			require.Equal(t, value, BaggageMember(imported, "value"))
			require.Equal(t, value, enrichment.String(imported, "value"))
			var record map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
			require.Equal(t, value, record["value"])
			spans := exports.GetSpans()
			require.Len(t, spans, 1)
			found := false
			for _, attr := range spans[0].Attributes {
				if attr.Key == "value" {
					found = true
					require.Equal(t, value, attr.Value.AsString())
				}
			}
			require.True(t, found)
		})
	}
}

func TestEnrichInvalidKeysAndUTF8RemainRejected(t *testing.T) {
	for _, attr := range []Attribute{
		StringAttribute("", "value"), StringAttribute("bad key", "value"),
		StringAttribute("a,b", "value"), StringAttribute("ключ", "value"),
		StringAttribute("value", "\xff"),
	} {
		// Arrange.
		ctx := context.Background()
		// Act.
		result := Enrich(ctx, attr)
		// Assert.
		require.Empty(t, enrichment.Values(result))
		require.Empty(t, baggage.FromContext(result).Members())
	}
}

func TestEnrichOverflowKeepsPrivateValuesAndReportsNoContent(t *testing.T) {
	// Arrange: a large raw value overflows transport but is valid local data.
	provider, exports := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "bounds", "enrichment")
	require.NoError(t, err)
	value := strings.Repeat("private-sentinel", 700)
	var logs bytes.Buffer
	// Act.
	ctx = Enrich(ctx, StringAttribute("a", value), StringAttribute("z", "later"))
	slog.New(ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)}).InfoContext(ctx, "local")
	end()
	require.NoError(t, provider.ForceFlush(context.Background()))
	// Assert: sorted prefix stops at a, while both local values remain available.
	require.Zero(t, baggage.FromContext(ctx).Len())
	require.Equal(t, value, enrichment.String(ctx, "a"))
	require.Equal(t, "later", enrichment.String(ctx, "z"))
	var record map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
	require.Equal(t, value, record["a"])
	spans := exports.GetSpans()
	require.Len(t, spans, 1)
	require.Len(t, spans[0].Events, 1)
	require.Equal(t, "metry.baggage.dropped", spans[0].Events[0].Name)
	for _, attr := range spans[0].Events[0].Attributes {
		require.NotContains(t, attr.Value.String(), "private-sentinel")
	}
	for _, attr := range spans[0].Attributes {
		if attr.Key == "a" {
			require.Equal(t, value, attr.Value.AsString())
		}
	}
	// Replacing the large member restores previously omitted z from the full ledger.
	ctx = Enrich(ctx, StringAttribute("a", "small"))
	require.Equal(t, "later", BaggageMember(ctx, "z"))
}

func TestEnrichSortedMemberRetention(t *testing.T) {
	// Arrange.
	attrs := make([]Attribute, 0, baggageutil.MaxMembers+5)
	for index := baggageutil.MaxMembers + 4; index >= 0; index-- {
		attrs = append(attrs, StringAttribute(fmt.Sprintf("k%03d", index), "x"))
	}
	// Act.
	ctx := Enrich(context.Background(), attrs...)
	// Assert.
	require.Equal(t, baggageutil.MaxMembers, baggage.FromContext(ctx).Len())
	require.Len(t, enrichment.Values(ctx), baggageutil.MaxMembers+5)
	require.Equal(t, "x", BaggageMember(ctx, "k000"))
	require.Equal(t, "x", BaggageMember(ctx, "k063"))
	require.Empty(t, BaggageMember(ctx, "k064"))
}
