package metry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/propagation"
	"github.com/skosovsky/metry/testutil"
)

func TestPropagation_ProtocolCarrierBoundary(t *testing.T) {
	ctx := context.Background()
	policy, policyErr := propagation.NewPolicy(
		propagation.PolicyConfig{
			IngressBaggageKeys: []string{"tenant_id", "score", "passed", "retrieval_top_k"},
			EgressBaggageKeys:  []string{"tenant_id", "score", "passed", "retrieval_top_k"},
		},
	)
	require.NoError(t, policyErr)
	provider, mem := metrytest.NewTestProvider(
		t,
		metry.WithPropagationPolicy(policy),
		metry.WithServiceName("propagation-e2e"),
	)

	ctx, end, err := provider.StartSpan(ctx, "producer", "send")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx,
		metry.TenantID("t-1"),
		metry.FloatAttribute("score", 0.91),
		metry.BoolAttribute("passed", true),
		metry.IntAttribute("retrieval_top_k", 5),
	)

	headers := map[string]any{"x-request-id": "req-99"}
	provider.InjectToMap(ctx, headers)
	end()

	consumerCtx := provider.ExtractFromMap(context.Background(), headers)
	assert.Equal(t, "req-99", headers["x-request-id"])
	assert.Equal(t, "t-1", metrytest.BaggageMember(consumerCtx, "tenant_id"))
	assert.Equal(t, "0.91", metrytest.BaggageMember(consumerCtx, "score"))
	assert.Equal(t, "true", metrytest.BaggageMember(consumerCtx, "passed"))
	assert.Equal(t, "5", metrytest.BaggageMember(consumerCtx, "retrieval_top_k"))

	_, consumerEnd, err := provider.StartSpan(consumerCtx, "consumer", "receive")
	require.NoError(t, err)
	consumerEnd()

	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.Len(t, spans, 2)
	producer := spans[0]
	consumer := spans[1]
	assert.Equal(t, "send", producer.Name)
	assert.Equal(t, codes.Unset, producer.Status.Code)
	assert.Equal(t, "receive", consumer.Name)
	assert.Equal(t, codes.Unset, consumer.Status.Code)
	assert.Equal(t, producer.SpanContext.TraceID(), consumer.SpanContext.TraceID())
	assert.Equal(t, producer.SpanContext.SpanID(), consumer.Parent.SpanID())
}

func TestAsyncHandle_LinkedSpanCallback(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("async-handle-e2e"))

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
