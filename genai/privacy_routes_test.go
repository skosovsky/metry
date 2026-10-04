package genai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestPrivacyBeforeFanoutProtectsBothIndependentSinks(t *testing.T) {
	for _, mode := range []struct {
		name   string
		option Option
		raw    bool
	}{
		{"redact", WithPayloadPolicy(RedactPayloadPolicy()), false},
		{"hash", WithPayloadPolicy(HashPayloadPolicy()), false},
		{"raw", WithRawPayloads(), true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			// Arrange: two SDK processors independently export one controlled boundary.
			first, second := testutil.NewInMemoryTraceExporter(), testutil.NewInMemoryTraceExporter()
			tracer := sdktrace.NewTracerProvider(
				sdktrace.WithSyncer(first.SDKSpanExporter()),
				sdktrace.WithSyncer(second.SDKSpanExporter()),
			)
			provider := metrytest.NewProviderWithDeps(tracer, sdkmetric.NewMeterProvider())
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			tracker, err := NewTrackerFromProvider(provider, mode.option)
			require.NoError(t, err)
			const secret = "private_marker_7"
			payload := Payload{InputMessages: []Message{{Role: secret, FinishReason: secret, Parts: []ContentPart{{
				Type: secret, Content: secret, ID: secret, Name: secret,
				Arguments: []byte(`{"` + secret + `":{"nested":"` + secret + `"}}`),
				Result:    []byte(`[{"` + secret + `":"` + secret + `"}]`),
			}}}}}
			// Act: content, schema metadata, dynamic keys, references and raw errors.
			require.NoError(
				t,
				tracker.RecordInteraction(
					context.Background(),
					Meta{Provider: secret, Operation: secret, RequestModel: secret, ResponseModel: secret,
						ServerAddress: secret, Purpose: secret, ErrorType: "timeout"},
					payload,
					Usage{InputTokens: Observe(0, UnitTokens, SourceMeasured), Purpose: secret, PriceReference: secret},
				),
			)
			runtime := tracker.Runtime().ForProvider(secret)
			_, end, startErr := runtime.StartTool(context.Background(), ToolCall{
				Name: secret, CallID: secret, Arguments: `{"` + secret + `":"` + secret + `"}`,
			})
			require.NoError(t, startErr)
			require.NoError(t, end.Finish(
				ToolResult{Result: `{"` + secret + `":"` + secret + `"}`, Err: errors.New(secret)},
			))
			_, operation, err := runtime.
				StartOperation(
					context.Background(),
					Operation{Name: secret, Model: secret, Purpose: secret},
					RecordingOptions{
						Identity: CausalIdentity{
							RunID:          secret,
							OperationID:    secret,
							StepID:         secret,
							AttemptID:      secret,
							Kind:           IdentityAttempt,
							PromptIdentity: secret,
						},
					},
				)
			require.NoError(t, err)
			require.ErrorIs(t, operation.Finish(OperationResult{ErrorType: secret}), ErrInvalidObservation)
			require.NoError(t, operation.Finish(OperationResult{ErrorType: "timeout"}))
			evaluation := successfulEvaluation()
			evaluation.Metric = EvaluationMetric(secret)
			evaluation.ObservationID, evaluation.Outcome, evaluation.Reasoning = secret, secret, secret
			evaluation.ArtifactReference = secret
			evaluation.Association = EvaluationAssociation{
				ExperimentID:     secret,
				DatasetReference: secret,
				CaseID:           secret,
				TrialID:          secret,
				TargetReference:  secret,
				GraderReference:  secret,
			}
			require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
			retrievalCtx, finishRetrieval, retrievalErr := tracker.StartRetrieval(context.Background(), secret,
				RetrievalRequest{Provider: secret, Source: secret, Query: secret})
			require.NoError(t, retrievalErr)
			tracker.RecordAgentStep(retrievalCtx, secret, secret, secret)
			tracker.RecordCacheHit(retrievalCtx, true, secret)
			require.NoError(t, tracker.RecordTraceIO(retrievalCtx, payload, payload))
			require.NoError(t, finishRetrieval.Finish(RetrievalResult{Status: OperationStatusOK, ReturnedChunks: 0}))
			origin, err := runtime.StartAsync(context.Background(), secret)
			require.NoError(t, err)
			require.NoError(
				t,
				runtime.RecordAsyncResult(
					context.Background(),
					origin,
					AsyncResult{Name: secret, Err: errors.New(secret)},
				),
			)
			require.NoError(t, tracker.RecordAsyncFeedback(context.Background(), origin, 0, secret))
			require.NoError(t, provider.ForceFlush(context.Background()))
			// Assert: no secret in any owned span/event/status field on either route.
			for _, sink := range []*testutil.InMemoryTraceExporter{first, second} {
				spans := sink.GetSpans()
				require.Len(t, spans, 8)
				priceReference := testutil.SpanStubStringAttr(t, spans[0], "metry.usage.price_reference")
				assertExportedPriceReference(t, priceReference, secret, mode.raw)
				for _, span := range spans {
					assertDeclaredWireFields(t, span.Attributes)
					for _, event := range span.Events {
						assertDeclaredWireFields(t, event.Attributes)
						assertDeclaredEvent(t, event.Name, event.Attributes)
					}
				}
				exported := fmt.Sprint(spans)
				if mode.raw {
					assert.Contains(t, exported, secret)
				} else {
					assert.NotContains(t, exported, secret)
				}
			}
			assert.Equal(t, first.GetSpans(), second.GetSpans())
			// Sanitizers must not change the host-owned input.
			assert.Equal(t, secret, payload.InputMessages[0].Role)
			assert.Equal(t, secret, payload.InputMessages[0].Parts[0].Name)
		})
	}
}

func assertExportedPriceReference(t *testing.T, value, secret string, raw bool) {
	t.Helper()
	assert.NotEmpty(t, value, "fixture must exercise price-reference export")
	if raw {
		assert.Equal(t, secret, value)
	} else {
		assert.NotContains(t, value, secret)
	}
}

func assertDeclaredEvent(t *testing.T, name string, attrs []attribute.KeyValue) {
	t.Helper()
	for _, event := range DefaultProfile().Events() {
		if event.Name != name {
			continue
		}
		assert.NotEmpty(t, event.Owner)
		for _, attr := range attrs {
			assert.True(t, slices.Contains(event.Fields, string(attr.Key)), "event %s field %s", name, attr.Key)
		}
		return
	}
	t.Errorf("undeclared controlled event: %s", name)
}

func assertDeclaredWireFields(t *testing.T, attrs []attribute.KeyValue) {
	t.Helper()
	fields := make(map[string]ProfileField)
	for _, field := range DefaultProfile().Fields {
		fields[field.Name] = field
	}
	for _, attr := range attrs {
		field, found := fields[string(attr.Key)]
		require.True(t, found, "undeclared owned field: %s", attr.Key)
		switch field.Encoding {
		case EncodingString, EncodingJSON:
			assert.Equal(t, attribute.STRING, attr.Value.Type(), "field: %s", attr.Key)
		case EncodingInteger:
			assert.Equal(t, attribute.INT64, attr.Value.Type(), "field: %s", attr.Key)
		case EncodingFloat:
			assert.Equal(t, attribute.FLOAT64, attr.Value.Type(), "field: %s", attr.Key)
		case EncodingBoolean:
			assert.Equal(t, attribute.BOOL, attr.Value.Type(), "field: %s", attr.Key)
		case EncodingFloatArray:
			assert.Equal(t, attribute.FLOAT64SLICE, attr.Value.Type(), "field: %s", attr.Key)
		default:
			t.Errorf("unsupported declared encoding for %s", attr.Key)
		}
	}
}

func TestMediaDescriptorsRejectInlineBodiesAndProtectReferences(t *testing.T) {
	// Arrange: references are metadata; inline bodies are forbidden even in raw mode.
	for _, body := range []string{"data:image/png;base64,private_marker_7", strings.Repeat("YQ==", 100)} {
		tracker, provider, exporter := newTestTracker(t, WithRawPayloads())
		// Act.
		err := tracker.RecordInteraction(context.Background(), testMeta(), Payload{
			SystemInstructions: []ContentPart{{Type: "image", Content: body}},
		}, Usage{})
		flushTestProvider(t, provider)
		// Assert.
		require.ErrorIs(t, err, ErrInvalidObservation)
		assert.Empty(t, exporter.GetSpans())
	}
	for _, policy := range []PayloadPolicy{RedactPayloadPolicy(), HashPayloadPolicy()} {
		// Arrange: a signed URL is opaque, never fetched and never exported raw.
		tracker, provider, exporter := newTestTracker(t, WithPayloadPolicy(policy))
		// Act.
		require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), Payload{
			SystemInstructions: []ContentPart{
				{Type: "image", Content: "https://example.invalid/private_marker_7?signature=private_marker_7"},
			},
		}, Usage{}))
		flushTestProvider(t, provider)
		// Assert.
		require.Len(t, exporter.GetSpans(), 1)
		assert.NotContains(t, fmt.Sprint(exporter.GetSpans()), "private_marker_7")
	}
}

func TestTextInputBoundIsAppliedBeforeCustomSanitizer(t *testing.T) {
	// Arrange.
	policy := &countingPayloadPolicy{calls: 0}
	cfg := buildRuntimeConfig(WithPayloadPolicy(policy))
	// Act.
	result := sanitizeTextWithConfig(strings.Repeat("x", maxPayloadInputBytes+1), cfg)
	// Assert.
	assert.Equal(t, redactedPayloadText, result)
	assert.Zero(t, policy.calls)
}

func TestFeedbackRejectsNonFiniteAndOversizedInputBeforeLinkedRecording(t *testing.T) {
	// Arrange.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads())
	handle := metrytest.AsyncHandleFromSpanContext(t, testutil.NewTestParentSpanContext(true))
	// Act.
	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.ErrorIs(
			t,
			tracker.RecordAsyncFeedback(context.Background(), handle, score, "private_marker_7"),
			ErrInvalidObservation,
		)
	}
	require.ErrorIs(t, tracker.RecordAsyncFeedback(context.Background(), handle, 0,
		strings.Repeat("x", maxPayloadInputBytes+1)), ErrInvalidObservation)
	flushTestProvider(t, provider)
	// Assert: no score or partial observation escapes on rejection.
	assert.Empty(t, exporter.GetSpans())
}
