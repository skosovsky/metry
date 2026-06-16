package metry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestTask14_FullQueueFlow(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("task14-e2e"))

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx,
		metry.TenantID("t-1"),
		metry.FloatAttribute("score", 0.91),
		metry.BoolAttribute("passed", true),
		metry.IntAttribute("retrieval_top_k", 5),
	)

	carrier := map[string]any{"order_id": "ord-99"}
	provider.InjectToMap(ctx, carrier)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	end()

	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	assert.Equal(t, "t-1", metrytest.BaggageMember(workerCtx, "tenant_id"))
	assert.Equal(t, "0.91", metrytest.BaggageMember(workerCtx, "score"))
	assert.Equal(t, "true", metrytest.BaggageMember(workerCtx, "passed"))
	assert.Equal(t, "5", metrytest.BaggageMember(workerCtx, "retrieval_top_k"))

	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)
	err = parsed.RecordLinkedOutcomeWithProvider(workerCtx, provider, "delivery.success",
		metry.TenantID("t-1"),
		metry.FloatAttribute("score", 0.91),
		metry.BoolAttribute("passed", true),
		metry.IntAttribute("retrieval_top_k", 5),
	)
	require.NoError(t, err)

	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.GreaterOrEqual(t, len(spans), 2)
	producer := spans[0]
	outcome := spans[len(spans)-1]
	assert.Equal(t, "enqueue", producer.Name)
	testutil.AssertSpanStubOkStatus(t, producer)
	assert.Equal(t, "delivery.success", outcome.Name)
	testutil.AssertSpanStubOkStatus(t, outcome)
	require.False(t, outcome.Parent.SpanID().IsValid())
	require.NotEmpty(t, outcome.Links)
	assert.Equal(t, producer.SpanContext.SpanID(), outcome.Links[0].SpanContext.SpanID())

	assert.Equal(t, "t-1", testutil.SpanStubStringAttr(t, outcome, "tenant_id"))
	assert.InDelta(t, 0.91, testutil.SpanStubFloat64Attr(t, outcome, "score"), 1e-9)
	assert.True(t, testutil.SpanStubBoolAttr(t, outcome, "passed"))
	assert.Equal(t, int64(5), testutil.SpanStubInt64Attr(t, outcome, "retrieval_top_k"))
}

func TestTask14_LinkedSpanCallbackFlow(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("task14-linked-span-e2e"))

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	require.NoError(t, err)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	end()

	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)
	err = parsed.RecordLinkedSpan(ctx, provider, "eval.result", func(w metry.LinkedSpanWriter) error {
		w.SetAttributes(metry.StringAttribute("metric", "faithfulness"))
		w.AddEvent("scored", metry.FloatAttribute("score", 0.88))
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	assert.Equal(t, "eval.result", spans[1].Name)
	testutil.AssertLinkBasedAsyncSpan(t, spans[1], spans[0])
	assert.Equal(t, "faithfulness", testutil.SpanStubStringAttr(t, spans[1], "metric"))
	testutil.AssertSpanStubOkStatus(t, spans[1])
}
