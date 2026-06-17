package metry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestGenAIScope_QueueWorkerFlow(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("genai-scope-e2e"))

	tracker, err := genai.NewTrackerFromProvider(provider)
	require.NoError(t, err)

	scope := genai.Scope{
		Provider:  "openai",
		Model:     "gpt-4o-mini",
		Operation: "chat",
		Purpose:   genai.PurposeGeneration,
	}

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	require.NoError(t, err)
	ctx = genai.WithScope(ctx, scope)
	ctx = metry.Enrich(ctx, metry.TenantID("t-scope"))

	carrier := map[string]any{"order_id": "ord-scope"}
	provider.InjectToMap(ctx, carrier)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	end()

	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	assert.Equal(t, "t-scope", metrytest.BaggageMember(workerCtx, "tenant_id"))
	assert.Equal(t, "openai", metrytest.BaggageMember(workerCtx, metry.GenAIBaggageProviderKey))
	assert.Equal(t, "gpt-4o-mini", metrytest.BaggageMember(workerCtx, metry.GenAIBaggageModelKey))

	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)

	workerCtx, workerEnd, err := provider.StartSpan(workerCtx, "worker", "consume")
	require.NoError(t, err)
	require.NoError(t, tracker.RecordOperation(workerCtx, scope, func(scopedCtx context.Context) error {
		return tracker.RecordInteraction(scopedCtx, genai.Meta{}, genai.Payload{}, genai.Usage{InputTokens: 1})
	}))
	require.NoError(t, parsed.RecordLinkedOutcomeWithProvider(workerCtx, provider, "delivery.success",
		metry.TenantID("t-scope"),
	))
	workerEnd()

	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.GreaterOrEqual(t, len(spans), 3)

	interaction := testutil.SpanByName(t, spans, "chat")
	assert.Equal(t, "openai", testutil.SpanStubStringAttr(t, interaction, genai.ProviderName))
	assert.Equal(t, "gpt-4o-mini", testutil.SpanStubStringAttr(t, interaction, genai.ResponseModel))
	assert.Equal(t, genai.PurposeGeneration, testutil.SpanStubStringAttr(t, interaction, genai.OperationPurpose))
	testutil.AssertSpanStubOkStatus(t, interaction)

	outcome := testutil.SpanByName(t, spans, "delivery.success")
	require.NotEmpty(t, outcome.Links)
	assert.Equal(t, "t-scope", testutil.SpanStubStringAttr(t, outcome, "tenant_id"))
	testutil.AssertSpanStubOkStatus(t, outcome)
}
