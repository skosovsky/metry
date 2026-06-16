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

func TestTask14_MetricsRegistryInWorkerContext(t *testing.T) {
	ctx := context.Background()
	memMetric := testutil.NewInMemoryMetricExporter()
	provider, err := metry.New(ctx,
		metry.WithServiceName("task14-metrics-e2e"),
		metry.WithMetricExporter(metrytest.MetryMetricExporter(memMetric)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	registry := metry.NewMetricsRegistry(provider)
	duration, err := registry.NewHistogram("agent_loop_duration", []float64{0.5, 1, 2, 4})
	require.NoError(t, err)
	require.True(t, duration.OK())

	steps, err := registry.NewCounter("agent_loop_steps_total")
	require.NoError(t, err)
	require.True(t, steps.OK())

	queueDepth, err := registry.NewGauge("queue_depth")
	require.NoError(t, err)
	require.True(t, queueDepth.OK())

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	require.NoError(t, err)
	ctx = metry.Enrich(ctx, metry.TenantID("t-metrics"))
	carrier := map[string]any{"job_id": "job-1"}
	provider.InjectToMap(ctx, carrier)
	end()

	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	assert.Equal(t, "t-metrics", metrytest.BaggageMember(workerCtx, "tenant_id"))

	duration.Record(workerCtx, 1.25, metry.Labels{"status": "ok"})
	steps.Add(workerCtx, 1, metry.Labels{"status": "ok"})
	queueDepth.Record(workerCtx, 3, metry.Labels{"queue": "jobs"})

	rm := metrytest.CollectResourceMetrics(t, provider, memMetric)

	assert.InDelta(t, 1.25, metrytest.Float64HistogramSum(t, rm, "agent_loop_duration"), 1e-9)
	assert.Equal(t, int64(1), metrytest.Int64SumValue(t, rm, "agent_loop_steps_total"))
	assert.Equal(t, "ok", metrytest.FirstInt64SumAttr(t, rm, "agent_loop_steps_total", "status"))
	assert.InDelta(t, 3, metrytest.GaugeFloat64Value(t, rm, "queue_depth"), 1e-9)
	assert.Equal(t, "jobs", metrytest.FirstGaugeAttr(t, rm, "queue_depth", "queue"))
}
