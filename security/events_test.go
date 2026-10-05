package security

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/diagnostic"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestRecordPrivacyBeforeTwoExporters(t *testing.T) {
	for _, mode := range []string{"default", "sanitized", "raw", "failure", "panic", "output_bound", "tiny_output"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			const secret = "secret-security-sentinel"
			config := diagnostic.Config{}
			switch mode {
			case "raw":
				config.Raw = true
			case "sanitized":
				config.Sanitizer = func(_ diagnostic.Channel, _ string) (string, error) { return "safe_reason", nil }
			case "failure":
				config.Sanitizer = func(_ diagnostic.Channel, _ string) (string, error) { return secret, assert.AnError }
			case "panic":
				config.Sanitizer = func(_ diagnostic.Channel, _ string) (string, error) { panic(secret) }
			case "tiny_output":
				config.MaxOutputBytes = 1
				config.Sanitizer = func(_ diagnostic.Channel, _ string) (string, error) { return "too_long", nil }
			case "output_bound":
				config.Sanitizer = func(_ diagnostic.Channel, _ string) (string, error) { return strings.Repeat(secret, 300), nil }
			}
			policy, err := diagnostic.NewPolicy(config)
			require.NoError(t, err)
			first, second := testutil.NewInMemoryTraceExporter(), testutil.NewInMemoryTraceExporter()
			tp := sdktrace.NewTracerProvider(
				sdktrace.WithSyncer(first.SDKSpanExporter()),
				sdktrace.WithSyncer(second.SDKSpanExporter()),
			)
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			provider := metrytest.NewProviderWithDeps(tp, noop.NewMeterProvider())
			// Use borrowed composition to install the same explicit policy as owned providers.
			provider, err = metry.FromSDK(
				metry.SDKDependencies{
					TracerProvider: tp,
					MeterProvider:  provider.SDK().MeterProvider,
					Propagator:     provider.SDK().Propagator,
				},
				metry.WithSDKDiagnosticPolicy(policy),
			)
			require.NoError(t, err)
			ctx, end, err := provider.StartSpan(context.Background(), "security-test", "host")
			require.NoError(t, err)
			// Act.
			err = Record(
				ctx,
				provider,
				Observation{
					Action:    ActionBlock,
					Validator: "validator",
					Reason:    secret,
					Code:      "POLICY_VIOLATION",
					Severity:  "HIGH",
					Shadow:    true,
				},
			)
			end()
			// Assert.
			require.NoError(t, err)
			assertPrivacySinks(t, mode, secret, first, second)
		})
	}
}

func TestRecordMetadataRejectedBeforeWrites(t *testing.T) {
	cases := []Observation{
		{
			Action:    "unknown",
			Validator: "validator",
		},
		{Action: ActionPass},
		{Action: ActionPass, Validator: "contains space"},
		{
			Action:    ActionPass,
			Validator: strings.Repeat("v", 129),
		},
		{Action: ActionPass, Validator: "v", Code: "secret\nvalue"},
		{Action: ActionPass, Validator: "v", Severity: "é"},
	}
	for i, observation := range cases {
		// Arrange.
		provider, exporter := metrytest.NewTestProvider(t)
		// Act.
		err := Record(context.Background(), provider, observation)
		flushSpans(t, provider)
		// Assert.
		require.ErrorIs(t, err, ErrMetadata, "case %d", i)
		assert.Empty(t, exporter.GetSpans())
	}
}

func TestRecordReasonRejectedBeforeCallbackAndWrites(t *testing.T) {
	for _, reason := range []string{strings.Repeat("x", 65537), string([]byte{0xff})} {
		// Arrange.
		calls := 0
		policy, err := diagnostic.NewPolicy(
			diagnostic.Config{
				Sanitizer: func(_ diagnostic.Channel, _ string) (string, error) { calls++; return "safe", nil },
			},
		)
		require.NoError(t, err)
		provider, exporter := metrytest.NewTestProvider(t, metry.WithDiagnosticPolicy(policy))
		ctx, end, err := provider.StartSpan(context.Background(), "test", "host")
		require.NoError(t, err)
		// Act.
		err = Record(ctx, provider, Observation{Action: ActionPass, Validator: "v", Reason: reason})
		end()
		flushSpans(t, provider)
		// Assert.
		require.ErrorIs(t, err, diagnostic.ErrInput)
		assert.Zero(t, calls)
		require.Len(t, exporter.GetSpans(), 1)
		assert.Empty(t, exporter.GetSpans()[0].Events)
	}
}

func TestRecordCreatesSpanAndOptionalAttributes(t *testing.T) {
	// Arrange.
	provider, exporter := metrytest.NewTestProvider(t)
	// Act.
	err := Record(context.Background(), provider, Observation{Action: ActionBlock, Validator: "validator"})
	flushSpans(t, provider)
	// Assert.
	require.NoError(t, err)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "security.intervention", spans[0].Name)
	require.Len(t, spans[0].Events, 1)
	assert.Len(t, spans[0].Events[0].Attributes, 3)
	assert.NotEqual(t, codes.Error, spans[0].Status.Code)
}

func TestRecordNilDependencies(t *testing.T) {
	// Arrange.
	observation := Observation{Action: ActionPass, Validator: "validator"}
	// Act and Assert.
	require.ErrorIs(t, Record(context.Background(), nil, observation), metry.ErrNilProvider)
	require.ErrorIs(
		t,
		Record(context.Background(), metrytest.NewProviderWithDeps(nil, nil), observation),
		metry.ErrNilTracerProvider,
	)
}

func TestRecordUnsampledParentMayDropEvent(t *testing.T) {
	// Arrange.
	provider, exporter := metrytest.NewTestProvider(t)
	ctx := provider.ExtractFromMap(
		context.Background(),
		map[string]any{"traceparent": "00-0123456789abcdef0123456789abcdef-0123456789abcdef-00"},
	)
	// Act.
	err := Record(ctx, provider, Observation{Action: ActionBlock, Validator: "validator"})
	flushSpans(t, provider)
	// Assert.
	require.NoError(t, err)
	assert.Empty(t, exporter.GetSpans())
}

func flushSpans(t *testing.T, provider *metry.Provider) {
	t.Helper()
	require.NoError(t, provider.ForceFlush(context.Background()))
}

func assertPrivacySinks(t *testing.T, mode, secret string, sinks ...*testutil.InMemoryTraceExporter) {
	t.Helper()
	for _, sink := range sinks {
		spans := sink.GetSpans()
		require.Len(t, spans, 1)
		require.Len(t, spans[0].Events, 1)
		assert.NotEqual(t, codes.Error, spans[0].Status.Code)
		event := spans[0].Events[0]
		assert.Equal(t, string(ActionBlock), testutil.SpanEventStringAttr(t, event.Attributes, Action))
		assert.True(t, testutil.SpanStubBoolAttr(t, testutil.AttrsStub(event.Attributes), ShadowMode))
		reason := ""
		for _, attr := range event.Attributes {
			if string(attr.Key) == Reason {
				reason = attr.Value.AsString()
			}
			if mode != "raw" {
				assert.NotContains(t, attr.Value.String(), secret)
			}
		}
		switch mode {
		case "default":
			assert.Empty(t, reason)
		case "raw":
			assert.Equal(t, secret, reason)
		case "sanitized":
			assert.Equal(t, "safe_reason", reason)
		default:
			assert.Empty(t, reason)
		}
		assert.NotContains(t, spans[0].Status.Description, secret)
	}
}
