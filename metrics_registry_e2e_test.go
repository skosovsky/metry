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

func TestMetricsRegistry_TraceSnapshotWorkerFlow(t *testing.T) {
	ctx := context.Background()
	memMetric := testutil.NewInMemoryMetricExporter()
	provider, err := metry.New(ctx,
		metry.WithServiceName("metrics-registry-e2e"),
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
	const tenantID = "t-metrics"
	ctx = metry.Enrich(ctx, metry.TenantID(tenantID))
	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	snapshotToken, err := snapshot.Marshal()
	require.NoError(t, err)
	end()

	parsedSnapshot, err := metry.ParseTraceSnapshot(snapshotToken)
	require.NoError(t, err)
	workerCtx, err := provider.ContextWithTraceSnapshot(context.Background(), parsedSnapshot)
	require.NoError(t, err)
	assert.Empty(t, metrytest.BaggageMember(workerCtx, "tenant_id"))
	workerCtx = metry.Enrich(workerCtx, metry.TenantID(tenantID))
	assert.Equal(t, tenantID, metrytest.BaggageMember(workerCtx, "tenant_id"))

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
