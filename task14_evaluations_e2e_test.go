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

func TestTask14_EvaluationsQueueFlow(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("task14-evaluations-e2e"))

	tracker, err := genai.NewTrackerFromProvider(provider)
	require.NoError(t, err)

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx, metry.TenantID("t-eval"))
	carrier := map[string]any{"job_id": "eval-1"}
	provider.InjectToMap(ctx, carrier)
	handle, err := metry.NewAsyncHandle(ctx)
	require.NoError(t, err)
	token, err := handle.Marshal()
	require.NoError(t, err)
	end()

	parsed, err := metry.ParseAsyncHandle(token)
	require.NoError(t, err)

	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	assert.Equal(t, "t-eval", metrytest.BaggageMember(workerCtx, "tenant_id"))

	err = tracker.RecordEvaluations(workerCtx, parsed, []genai.Evaluation{
		{
			Metric: genai.EvaluationFaithfulness,
			Score:  0.88,
		},
	})
	require.NoError(t, err)

	require.NoError(t, provider.ForceFlush(ctx))

	spans := mem.GetSpans()
	require.GreaterOrEqual(t, len(spans), 2)
	producer := spans[0]
	evalSpan := testutil.SpanByName(t, spans, "llm_evaluations")
	testutil.AssertLinkBasedAsyncSpan(t, evalSpan, producer)
	testutil.AssertSpanStubOkStatus(t, evalSpan)
	require.Len(t, evalSpan.Events, 1)
	assert.Equal(t, "evaluation", evalSpan.Events[0].Name)
	assert.InDelta(
		t,
		0.88,
		testutil.SpanEventFloat64Attr(t, evalSpan.Events[0].Attributes, genai.EvaluationScore),
		1e-9,
	)
	assert.Equal(
		t,
		string(genai.EvaluationFaithfulness),
		testutil.SpanEventStringAttr(t, evalSpan.Events[0].Attributes, genai.EvaluationMetricName),
	)
}
