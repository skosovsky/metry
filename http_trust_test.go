package metry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	otelprop "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/metrytest"
	metryprop "github.com/skosovsky/metry/propagation"
)

func TestHTTPTrust_RealTransportBaggageSamplingAndLogs(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		allow, sampled, reject bool
	}{
		{name: "default_sampled", sampled: true},
		{name: "default_unsampled"},
		{name: "allowlist_sampled", allow: true, sampled: true},
		{name: "allowlist_unsampled", allow: true},
		{name: "reject_sampled", sampled: true, reject: true},
		{name: "reject_unsampled", reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			policy := trustPolicy(t, tc.allow, tc.reject)
			provider, exporter := metrytest.NewTestProvider(
				t,
				metry.WithPropagationPolicy(policy),
				metry.WithTraceRatio(1),
			)
			parent := trustParent(tc.sampled)
			var logs bytes.Buffer
			logger := slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)})
			observed := make(chan trace.SpanContext, 1)
			scopes := make(chan bool, 1)
			server := httptest.NewServer(
				metry.HTTPHandler(provider, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					sc := trace.SpanContextFromContext(r.Context())
					logger.InfoContext(r.Context(), "server-observed")
					_, scope := genai.ScopeFromContext(r.Context())
					observed <- sc
					scopes <- scope
					w.WriteHeader(http.StatusNoContent)
				}), "trust-http"),
			)
			t.Cleanup(server.Close)
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			otelprop.TraceContext{}.Inject(
				trace.ContextWithRemoteSpanContext(context.Background(), parent),
				otelprop.HeaderCarrier(request.Header),
			)
			request.Header.Set("Baggage", trustBaggage(t))
			// Act.
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			server.Close()
			require.NoError(t, provider.ForceFlush(context.Background()))
			sc := <-observed
			// Assert.
			require.Equal(t, http.StatusNoContent, response.StatusCode)
			require.True(t, sc.IsValid())
			require.False(t, <-scopes)
			assertTrustRecord(t, logs.Bytes(), sc, tc.allow)
			spans := exporter.GetSpans()
			if tc.reject {
				require.NotEqual(t, parent.TraceID(), sc.TraceID())
				require.True(t, sc.IsSampled())
				require.Len(t, spans, 1)
				require.Equal(t, sc, spans[0].SpanContext)
				require.False(t, spans[0].Parent.IsValid())
			} else {
				require.Equal(t, parent.TraceID(), sc.TraceID())
				require.Equal(t, tc.sampled, sc.IsSampled())
				if tc.sampled {
					require.Len(t, spans, 1)
					require.Equal(t, sc, spans[0].SpanContext)
					require.Equal(t, parent.SpanID(), spans[0].Parent.SpanID())
					require.True(t, spans[0].Parent.IsRemote())
				} else {
					require.Empty(t, spans)
				}
			}
		})
	}
}

func trustParent(sampled bool) trace.SpanContext {
	flags := trace.TraceFlags(0)
	if sampled {
		flags = trace.FlagsSampled
	}
	return trace.NewSpanContext(
		trace.SpanContextConfig{
			TraceID:    trace.TraceID{1, 2, 3},
			SpanID:     trace.SpanID{4, 5, 6},
			TraceFlags: flags,
			Remote:     true,
		},
	)
}

func trustBaggage(t *testing.T) string {
	t.Helper()
	specs := []struct{ key, value, kind string }{
		{"tenant", "remote", "string"}, {"count", "42", "int"}, {"ratio", "1.25", "float"}, {"enabled", "true", "bool"},
		{"msg", "spoof-message", "string"}, {"trace_id", "spoof-trace", "string"}, {"host.key", "spoof-host", "string"},
		{metry.GenAIBaggageModelKey, "spoof-model", "string"},
	}
	members := make([]baggage.Member, 0, len(specs))
	for _, spec := range specs {
		prop, err := baggage.NewKeyValuePropertyRaw("metry.attr.type", spec.kind)
		require.NoError(t, err)
		member, err := baggage.NewMemberRaw(spec.key, spec.value, prop)
		require.NoError(t, err)
		members = append(members, member)
	}
	bag, err := baggage.New(members...)
	require.NoError(t, err)
	return bag.String()
}

func trustPolicy(t *testing.T, allow, reject bool) metryprop.Policy {
	t.Helper()
	cfg := metryprop.PolicyConfig{RejectRemoteTrace: reject}
	if allow {
		cfg.IngressBaggageKeys = []string{
			"tenant",
			"count",
			"ratio",
			"enabled",
			"msg",
			"trace_id",
			"host.key",
			metry.GenAIBaggageModelKey,
		}
		cfg.ReservedKeys = []string{"host.key", metry.GenAIBaggageModelKey}
	}
	policy, err := metryprop.NewPolicy(cfg)
	require.NoError(t, err)
	return policy
}

func assertTrustRecord(t *testing.T, raw []byte, sc trace.SpanContext, allow bool) {
	t.Helper()
	var record map[string]any
	require.NoError(t, json.Unmarshal(raw, &record))
	require.Equal(t, "server-observed", record["msg"])
	require.Equal(t, sc.TraceID().String(), record["trace_id"])
	require.Equal(t, sc.SpanID().String(), record["span_id"])
	require.NotContains(t, record, "host.key")
	require.NotContains(t, record, metry.GenAIBaggageModelKey)
	if allow {
		require.Equal(t, "remote", record["tenant"])
		require.IsType(t, float64(0), record["count"])
		require.InDelta(t, 42, record["count"], 0)
		require.IsType(t, float64(0), record["ratio"])
		require.InDelta(t, 1.25, record["ratio"], 0)
		require.Equal(t, true, record["enabled"])
	} else {
		for _, key := range []string{"tenant", "count", "ratio", "enabled"} {
			require.NotContains(t, record, key)
		}
	}
}
