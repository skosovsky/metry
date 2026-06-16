package metry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/baggage"
)

func TestEnrich_GenAISemanticConstructors(t *testing.T) {
	ctx := Enrich(context.Background(),
		GenAIProvider("openai"),
		GenAIModel("gpt-4o-mini"),
		GenAIOperation("chat"),
		GenAIPurpose("generation"),
	)
	assert.Equal(t, "openai", BaggageMember(ctx, GenAIBaggageProviderKey))
	assert.Equal(t, "gpt-4o-mini", BaggageMember(ctx, GenAIBaggageModelKey))
	assert.Equal(t, "chat", BaggageMember(ctx, GenAIBaggageOperationKey))
	assert.Equal(t, "generation", BaggageMember(ctx, GenAIBaggagePurposeKey))
}

func TestEnrich_SkipsEmptySemanticIDs(t *testing.T) {
	ctx := Enrich(context.Background(), TenantID(""), SubjectID("job-1"))
	assert.Equal(t, "job-1", BaggageMember(ctx, "subject_id"))
	assert.Empty(t, BaggageMember(ctx, "tenant_id"))
}

func TestEnrich_UpdatesBaggage(t *testing.T) {
	ctx := Enrich(context.Background(), TenantID("t-1"), PatientID("p-9"))
	assert.Equal(t, "t-1", BaggageMember(ctx, "tenant_id"))
	assert.Equal(t, "p-9", BaggageMember(ctx, "patient_id"))
}

func TestEnrich_WithoutActiveSpan_NoSpanExport(t *testing.T) {
	provider, mem := newTestProvider(t)
	ctx := Enrich(context.Background(), TenantID("t-no-span"))
	assert.Equal(t, "t-no-span", BaggageMember(ctx, "tenant_id"))
	require.NoError(t, provider.ForceFlush(context.Background()))
	assert.Empty(t, mem.GetSpans(), "enrichSpan must no-op without recording span")
}

func TestEnrich_FloatAndBoolAttributes(t *testing.T) {
	provider, mem := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "req")
	require.NoError(t, err)
	ctx = Enrich(ctx, FloatAttribute("score", 0.85), BoolAttribute("shadow_mode", true))
	end()
	require.NoError(t, provider.ForceFlush(context.Background()))

	assert.Equal(t, "0.85", BaggageMember(ctx, "score"))
	assert.Equal(t, "true", BaggageMember(ctx, "shadow_mode"))

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := spans[0].Attributes
	var foundFloat, foundBool bool
	for _, kv := range attrs {
		switch string(kv.Key) {
		case "score":
			foundFloat = true
			assert.InDelta(t, 0.85, kv.Value.AsFloat64(), 1e-9)
		case "shadow_mode":
			foundBool = true
			assert.True(t, kv.Value.AsBool())
		}
	}
	assert.True(t, foundFloat)
	assert.True(t, foundBool)
}

func TestEnrich_IntAttribute(t *testing.T) {
	provider, mem := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "req")
	require.NoError(t, err)
	ctx = Enrich(ctx, IntAttribute("retrieval_top_k", 5))
	end()
	require.NoError(t, provider.ForceFlush(context.Background()))

	assert.Equal(t, "5", BaggageMember(ctx, "retrieval_top_k"))

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	found := false
	for _, kv := range spans[0].Attributes {
		if string(kv.Key) == "retrieval_top_k" {
			found = true
			assert.Equal(t, int64(5), kv.Value.AsInt64())
		}
	}
	assert.True(t, found)
}

func TestEnrich_SkipsInvalidBaggageMember_DoesNotBreakOthers(t *testing.T) {
	ctx := Enrich(context.Background(), newAttribute("invalid key /", "x"), TenantID("t-2"))
	assert.Equal(t, "t-2", BaggageMember(ctx, "tenant_id"))
}

func TestEnrich_SkipsInvalidKey_InSpanAndSlog(t *testing.T) {
	provider, mem := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "req")
	require.NoError(t, err)
	ctx = Enrich(ctx, newAttribute("invalid key /", "x"), TenantID("t-3"))
	end()
	require.NoError(t, provider.ForceFlush(context.Background()))

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	for _, kv := range spans[0].Attributes {
		assert.NotEqual(t, "invalid key /", string(kv.Key))
	}
	assert.Equal(t, "t-3", BaggageMember(ctx, "tenant_id"))
}

func TestEnrich_ActiveSpan_ReceivesAttributes(t *testing.T) {
	provider, mem := newTestProvider(t)
	ctx, end, err := provider.StartSpan(context.Background(), "test", "req")
	require.NoError(t, err)
	Enrich(ctx, DoctorID("d-7"))
	end()
	require.NoError(t, provider.ForceFlush(context.Background()))

	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	attrs := spans[0].Attributes
	found := false
	for _, kv := range attrs {
		if string(kv.Key) == "doctor_id" {
			found = true
			assert.Equal(t, "d-7", kv.Value.AsString())
		}
	}
	assert.True(t, found)
}

func TestEnrich_AccumulatesBaggageAcrossCalls(t *testing.T) {
	ctx := Enrich(context.Background(), TenantID("t-1"))
	ctx = Enrich(ctx, PatientID("p-1"))

	assert.Equal(t, "t-1", BaggageMember(ctx, "tenant_id"))
	assert.Equal(t, "p-1", BaggageMember(ctx, "patient_id"))
}

func TestEnrich_WithoutReassignment_DoesNotUpdateBaggage(t *testing.T) {
	ctx := Enrich(context.Background(), TenantID("t-1"))
	Enrich(ctx, PatientID("p-1"))

	assert.Equal(t, "t-1", BaggageMember(ctx, "tenant_id"))
	assert.Empty(t, BaggageMember(ctx, "patient_id"))
}

func TestContextHandler_AddsEnrichAttrsAndTraceIDs(t *testing.T) {
	provider, _ := newTestProvider(t)
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(ContextHandler{Handler: base})

	ctx, end, err := provider.StartSpan(context.Background(), "test", "req")
	require.NoError(t, err)
	ctx = Enrich(ctx, TenantID("t-99"))
	logger.InfoContext(ctx, "hello")
	end()

	line := strings.TrimSpace(buf.String())
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &payload))
	assert.Equal(t, "t-99", payload["tenant_id"])
	assert.NotEmpty(t, payload["trace_id"])
	assert.NotEmpty(t, payload["span_id"])
}

func TestContextHandler_FloatAttributeUsesTypedSlog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)})

	ctx := Enrich(context.Background(), FloatAttribute("score", 0.5))
	logger.InfoContext(ctx, "scored")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.InDelta(t, 0.5, payload["score"], 1e-9)
}

func TestContextHandler_BoolAttributeUsesTypedSlog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)})

	ctx := Enrich(context.Background(), BoolAttribute("active", true))
	logger.InfoContext(ctx, "flagged")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.Equal(t, true, payload["active"])
}

func TestContextHandler_IntAttributeUsesTypedSlog(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)})

	ctx := Enrich(context.Background(), IntAttribute("retrieval_top_k", 5))
	logger.InfoContext(ctx, "indexed")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.InDelta(t, float64(5), payload["retrieval_top_k"], 1e-9)
}

func TestContextHandler_IgnoresForeignBaggage(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)})

	foreign, err := baggage.NewMember("foreign_key", "foreign_value")
	require.NoError(t, err)
	b, err := baggage.New()
	require.NoError(t, err)
	b, err = b.SetMember(foreign)
	require.NoError(t, err)
	ctx := baggage.ContextWithBaggage(context.Background(), b)
	ctx = Enrich(ctx, TenantID("t-1"))
	logger.InfoContext(ctx, "hello")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.Equal(t, "t-1", payload["tenant_id"])
	_, hasForeign := payload["foreign_key"]
	assert.False(t, hasForeign)
}

func TestContextHandler_WithoutEnrich_StillAddsTraceIDs(t *testing.T) {
	provider, _ := newTestProvider(t)
	var buf bytes.Buffer
	logger := slog.New(ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)})

	ctx, end, err := provider.StartSpan(context.Background(), "test", "req")
	require.NoError(t, err)
	logger.InfoContext(ctx, "ping")
	end()

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.NotEmpty(t, payload["trace_id"])
	_, hasTenant := payload["tenant_id"]
	assert.False(t, hasTenant)
}
