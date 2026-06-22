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

func TestGenAIScope_TraceSnapshotWorkerFlow(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("genai-scope-e2e"))

	runtime, err := genai.NewRuntimeFromProvider(provider)
	require.NoError(t, err)

	scope := genai.Scope{
		Provider:  "",
		Model:     "gpt-4o-mini",
		Operation: "chat",
		Purpose:   genai.PurposeGeneration,
	}

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	require.NoError(t, err)
	const tenantID = "t-scope"
	ctx = genai.WithScope(ctx, scope)
	ctx = metry.Enrich(ctx, metry.TenantID(tenantID))
	runtime = runtime.ForProvider("openai")

	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	snapshotToken, err := snapshot.Marshal()
	require.NoError(t, err)
	handle, err := runtime.StartAsync(ctx, "delivery.enqueue")
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	end()

	parsedSnapshot, err := metry.ParseTraceSnapshot(snapshotToken)
	require.NoError(t, err)
	workerCtx, err := provider.ContextWithTraceSnapshot(context.Background(), parsedSnapshot)
	require.NoError(t, err)
	assert.Empty(t, metrytest.BaggageMember(workerCtx, "tenant_id"))
	assert.Empty(t, metrytest.BaggageMember(workerCtx, metry.GenAIBaggageProviderKey))
	assert.Empty(t, metrytest.BaggageMember(workerCtx, metry.GenAIBaggageModelKey))
	consumerCtx := genai.WithScope(workerCtx, scope)
	consumerCtx = metry.Enrich(consumerCtx, metry.TenantID(tenantID))

	consumerCtx, consumerEnd, err := provider.StartSpan(consumerCtx, "consumer", "consume")
	require.NoError(t, err)
	require.NoError(t, runtime.RecordOperation(consumerCtx, genai.Operation{
		Name:    scope.Operation,
		Model:   scope.Model,
		Purpose: scope.Purpose,
	}, genai.OperationResult{
		Status: genai.OperationStatusOK,
		Usage:  genai.Usage{InputTokens: 1},
	}))
	require.NoError(t, runtime.RecordAsyncTokenResult(consumerCtx, token, genai.AsyncResult{
		Name:   "delivery.success",
		Status: genai.OperationStatusOK,
		Attributes: []metry.Attribute{
			metry.TenantID(tenantID),
		},
	}))
	consumerEnd()

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
	assert.Equal(t, tenantID, testutil.SpanStubStringAttr(t, outcome, "tenant_id"))
	testutil.AssertSpanStubOkStatus(t, outcome)
}
