package genai

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
	otelprop "go.opentelemetry.io/otel/propagation"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	metryprop "github.com/skosovsky/metry/propagation"
	"github.com/skosovsky/metry/testutil"
)

func privateTestScope() Scope {
	return Scope{
		Provider: "private-provider-123", Model: "private-model-123",
		Operation: "private-operation-123", Purpose: "private-customer-123",
	}
}

func declaredScopeProfile(scope Scope) Profile {
	profile := DefaultProfile()
	for i := range profile.Dimensions {
		switch profile.Dimensions[i].Key {
		case ProviderName:
			profile.Dimensions[i].Values = []string{scope.Provider}
		case RequestModel, ResponseModel:
			profile.Dimensions[i].Values = []string{scope.Model}
		case OperationName:
			profile.Dimensions[i].Values = []string{scope.Operation}
		case OperationPurpose:
			profile.Dimensions[i].Values = []string{scope.Purpose}
		}
	}
	return profile
}

//nolint:gocognit // Keep the AAA privacy matrix and signal assertions together.
func TestScopePrivacyAcrossEntryPointsAndPolicies(t *testing.T) {
	for _, mode := range []string{"default", "declared", "raw"} {
		for _, entry := range []string{"meta", "package", "tracker"} {
			t.Run(mode+"/"+entry, func(t *testing.T) {
				// Arrange: override the helper's safe-label profile explicitly.
				scope := privateTestScope()
				profile := DefaultProfile()
				if mode == "declared" {
					profile = declaredScopeProfile(scope)
				}
				opts := []Option{WithProfile(profile)}
				if mode == "raw" {
					opts = append(opts, WithRawPayloads())
				}
				tracker, provider, metrics, exports := newTestTrackerWithMetrics(t, opts...)
				ctx, end, err := provider.StartSpan(context.Background(), "host", "parent")
				require.NoError(t, err)
				var logs bytes.Buffer
				logger := slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)})
				meta := Meta{Provider: scope.Provider, RequestModel: scope.Model,
					Operation: scope.Operation, Purpose: scope.Purpose}
				// Act.
				switch entry {
				case "package":
					ctx = WithScope(ctx, scope)
					meta = Meta{}
				case "tracker":
					ctx = tracker.WithScope(ctx, scope)
					meta = Meta{}
				}
				logger.InfoContext(ctx, "parent observation")
				require.NoError(t, tracker.RecordInteraction(ctx, meta, Payload{},
					Usage{InputTokens: Observe(1, UnitTokens, SourceMeasured)}))
				operation := Operation{
					Name: scope.Operation, Model: scope.Model, Purpose: scope.Purpose, Locality: ExecutionLocal,
				}
				if entry != "meta" {
					operation.Model, operation.Purpose = "", ""
				}
				child, handle, err := tracker.Runtime().ForProvider(scope.Provider).
					StartOperation(ctx, operation, RecordingOptions{})
				require.NoError(t, err)
				logger.InfoContext(child, "child observation")
				require.NoError(t, handle.Finish(OperationResult{Status: OperationStatusOK}))
				end()
				flushTestProvider(t, provider)
				// Assert: scope creation never enriches logs, parent or transport,
				// even when the emitting tracker has explicit raw permission.
				for _, value := range []string{scope.Provider, scope.Model, scope.Operation, scope.Purpose} {
					require.NotContains(t, logs.String(), value)
				}
				require.Empty(t, baggage.FromContext(ctx).Members())
				policy, err := metryprop.NewPolicy(metryprop.PolicyConfig{EgressBaggageKeys: []string{
					scopeBaggageProvider, scopeBaggageModel, scopeBaggageOperation, scopeBaggagePurpose,
				}})
				require.NoError(t, err)
				carrier := otelprop.MapCarrier{}
				metryprop.Wrap(otelprop.Baggage{}, policy).Inject(ctx, carrier)
				require.Empty(t, carrier.Get("baggage"))
				spans := exports.GetSpans()
				require.Len(t, spans, 3)
				for _, span := range spans {
					data, err := json.Marshal(span)
					require.NoError(t, err)
					if span.Name == "parent" || mode == "default" {
						for _, value := range []string{scope.Provider, scope.Model, scope.Operation, scope.Purpose} {
							require.NotContains(t, string(data), value)
						}
					} else {
						require.Equal(t, scope.Provider, testutil.SpanStubStringAttr(t, span, ProviderName))
						require.Equal(t, scope.Model, testutil.SpanStubStringAttr(t, span, RequestModel))
						require.Equal(t, scope.Operation, testutil.SpanStubStringAttr(t, span, OperationName))
						require.Equal(t, scope.Purpose, testutil.SpanStubStringAttr(t, span, OperationPurpose))
					}
				}
				if mode == "default" {
					data, err := json.Marshal(metrytest.CollectResourceMetrics(t, provider, metrics))
					require.NoError(t, err)
					for _, value := range []string{scope.Provider, scope.Model, scope.Operation, scope.Purpose} {
						require.NotContains(t, string(data), value)
					}
				}
				if entry != "meta" {
					original, ok := ScopeFromContext(ctx)
					require.True(t, ok)
					require.Equal(t, scope, original)
				}
			})
		}
	}
}

func TestScopeEmitterPolicyIsIndependentOfCreator(t *testing.T) {
	// Arrange.
	scope := privateTestScope()
	raw, _, _ := newTestTracker(t, WithProfile(DefaultProfile()), WithRawPayloads())
	safe, provider, exports := newTestTracker(t, WithProfile(DefaultProfile()))
	ctx := raw.WithScope(context.Background(), scope)
	// Act.
	require.NoError(t, safe.RecordInteraction(ctx, Meta{}, Payload{}, Usage{}))
	flushTestProvider(t, provider)
	// Assert.
	data, err := json.Marshal(exports.GetSpans())
	require.NoError(t, err)
	for _, value := range []string{scope.Provider, scope.Model, scope.Operation, scope.Purpose} {
		require.NotContains(t, string(data), value)
	}
	got, ok := ScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, scope, got)
}

func TestPrivateScopeMergeAndTrustedFallback(t *testing.T) {
	// Arrange: ordinary remote values must first pass the ingress policy.
	policy, err := metryprop.NewPolicy(metryprop.PolicyConfig{
		IngressBaggageKeys: []string{scopeBaggageProvider, scopeBaggageModel},
	})
	require.NoError(t, err)
	remote := otelprop.MapCarrier{"baggage": scopeBaggageProvider + "=remote-provider," +
		scopeBaggageModel + "=remote-model," + scopeBaggagePurpose + "=untrusted-purpose"}
	ctx := metryprop.Wrap(otelprop.Baggage{}, policy).Extract(context.Background(), remote)
	// Act: partial calls merge private local defaults; later direct enrichment
	// remains a fallback and cannot overwrite the private value.
	first := WithScope(ctx, Scope{Model: "private-model", Purpose: "private-purpose"})
	second := WithScope(first, Scope{Operation: "chat", Model: "replacement"})
	second = metry.Enrich(second, metry.GenAIModel("later-enrichment"))
	got, ok := ScopeFromContext(second)
	// Assert.
	require.True(t, ok)
	require.Equal(t, Scope{Provider: "remote-provider", Model: "replacement",
		Operation: "chat", Purpose: "private-purpose"}, got)
	previous, ok := ScopeFromContext(first)
	require.True(t, ok)
	require.Equal(t, "private-model", previous.Model)
	original, ok := ScopeFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, Scope{Provider: "remote-provider", Model: "remote-model"}, original)
	require.Same(t, second, WithScope(second, Scope{}))
	meta := mergeMetaFromScope(second, Meta{Provider: "explicit-provider", RequestModel: "explicit-model",
		Operation: "explicit-operation", Purpose: "explicit-purpose"})
	require.Equal(t, "explicit-provider", meta.Provider)
	require.Equal(t, "explicit-model", meta.RequestModel)
	require.Equal(t, "explicit-operation", meta.Operation)
	require.Equal(t, "explicit-purpose", meta.Purpose)
	// Arbitrary OTel baggage is not admitted to defaults.
	foreign := otelprop.Baggage{}.Extract(context.Background(), remote)
	_, ok = ScopeFromContext(foreign)
	require.False(t, ok)
	var tracker *Tracker
	nilReceiver := tracker.WithScope(context.Background(), privateTestScope())
	require.Equal(t, privateTestScope(), mergeScopeFromTest(nilReceiver, t))
}

func mergeScopeFromTest(ctx context.Context, t *testing.T) Scope {
	t.Helper()
	scope, ok := ScopeFromContext(ctx)
	require.True(t, ok)
	return scope
}
