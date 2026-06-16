package propagation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
)

func TestProviderInjectExtractMap_WithEnrich_RoundTripBaggage(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("propagation-blackbox"))

	ctx, end, err := provider.StartSpan(ctx, "test", "producer")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx, metry.SubjectID("job-42"))
	end()

	carrier := map[string]any{"order_id": "ord-1"}
	provider.InjectToMap(ctx, carrier)

	outCtx := provider.ExtractFromMap(context.Background(), carrier)
	_, workerEnd, err := provider.StartSpan(outCtx, "worker", "consume")
	require.NoError(t, err)
	workerEnd()
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
	assert.Equal(t, "job-42", metry.BaggageMember(outCtx, "subject_id"))
	assert.Equal(t, "ord-1", carrier["order_id"])
}

func TestProviderInjectExtractMap_PreservesTypedBaggage(t *testing.T) {
	ctx := context.Background()
	provider, _ := metrytest.NewTestProvider(t, metry.WithServiceName("propagation-typed"))

	ctx, end, err := provider.StartSpan(ctx, "test", "producer")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx, metry.FloatAttribute("score", 0.75), metry.BoolAttribute("active", true))
	end()

	carrier := map[string]any{}
	provider.InjectToMap(ctx, carrier)

	outCtx := provider.ExtractFromMap(context.Background(), carrier)
	assert.Equal(t, "0.75", metry.BaggageMember(outCtx, "score"))
	assert.Equal(t, "true", metry.BaggageMember(outCtx, "active"))
}

func TestProviderInjectExtractMap_PreservesIntBaggage(t *testing.T) {
	ctx := context.Background()
	provider, _ := metrytest.NewTestProvider(t, metry.WithServiceName("propagation-int"))

	ctx, end, err := provider.StartSpan(ctx, "test", "producer")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx, metry.IntAttribute("retrieval_top_k", 5))
	end()

	carrier := map[string]any{}
	provider.InjectToMap(ctx, carrier)

	outCtx := provider.ExtractFromMap(context.Background(), carrier)
	assert.Equal(t, "5", metry.BaggageMember(outCtx, "retrieval_top_k"))
}

func TestProviderInjectExtractMap_ContextHandlerTypedFieldsAfterRoundTrip(t *testing.T) {
	ctx := context.Background()
	provider, _ := metrytest.NewTestProvider(t, metry.WithServiceName("propagation-contexthandler"))

	ctx, end, err := provider.StartSpan(ctx, "test", "producer")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx,
		metry.FloatAttribute("score", 0.85),
		metry.BoolAttribute("passed", true),
		metry.IntAttribute("retrieval_top_k", 5),
	)
	end()

	carrier := map[string]any{}
	provider.InjectToMap(ctx, carrier)
	outCtx := provider.ExtractFromMap(context.Background(), carrier)

	var buf bytes.Buffer
	logger := slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)})
	logger.InfoContext(outCtx, "worker")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.InDelta(t, 0.85, payload["score"], 1e-9)
	assert.Equal(t, true, payload["passed"])
	assert.InDelta(t, float64(5), payload["retrieval_top_k"], 1e-9)
}

func TestProviderInjectExtractMap_TraceContextViaWorkerSpan(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("propagation-trace"))

	ctx, end, err := provider.StartSpan(ctx, "test", "producer")
	require.NoError(t, err)
	end()

	carrier := map[string]any{"biz": "payload"}
	provider.InjectToMap(ctx, carrier)
	outCtx := provider.ExtractFromMap(context.Background(), carrier)

	_, workerEnd, err := provider.StartSpan(outCtx, "worker", "job")
	require.NoError(t, err)
	workerEnd()
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
	assert.Equal(t, spans[0].SpanContext.SpanID(), spans[1].Parent.SpanID())
}
