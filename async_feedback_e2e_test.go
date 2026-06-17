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

func TestAsyncFeedback_QueueWorkerFlow(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("async-feedback-e2e"))

	tracker, err := genai.NewTrackerFromProvider(provider)
	require.NoError(t, err)

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx, metry.TenantID("t-feedback"))
	carrier := map[string]any{"job_id": "fb-1"}
	provider.InjectToMap(ctx, carrier)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	end()

	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)

	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	assert.Equal(t, "t-feedback", metrytest.BaggageMember(workerCtx, "tenant_id"))

	err = tracker.RecordAsyncFeedback(workerCtx, parsed, 0.95, "helpful")
	require.NoError(t, err)

	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.GreaterOrEqual(t, len(spans), 2)
	producer := spans[0]
	feedback := testutil.SpanByName(t, spans, "user_feedback")
	testutil.AssertLinkBasedAsyncSpan(t, feedback, producer)
	testutil.AssertSpanStubOkStatus(t, feedback)
	assert.InDelta(t, 0.95, testutil.SpanStubFloat64Attr(t, feedback, genai.EvaluationScore), 1e-9)
}
