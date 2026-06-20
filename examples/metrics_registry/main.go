package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func run() int {
	ctx := context.Background()
	metricMem := testutil.NewInMemoryMetricExporter()
	provider, err := metry.New(ctx,
		metry.WithServiceName("metrics-registry-example"),
		metry.WithMetricExporter(metrytest.MetryMetricExporter(metricMem)),
	)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	duration, steps, queueDepth, err := registerMetrics(provider)
	if err != nil {
		log.Println(err)
		return 1
	}
	const elapsedSeconds = 1.2
	const queueDepthValue = 3.0

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	if err != nil {
		log.Println(err)
		return 1
	}
	const tenantID = "t-metrics"
	ctx = metry.Enrich(ctx, metry.TenantID(tenantID))
	snapshotToken, err := captureTraceSnapshotToken(ctx)
	if err != nil {
		log.Println("capture trace snapshot:", err)
		return 1
	}
	end()

	workerCtx, err := restoreTraceSnapshot(context.Background(), provider, snapshotToken)
	if err != nil {
		log.Println("restore trace snapshot:", err)
		return 1
	}
	workerCtx = metry.Enrich(workerCtx, metry.TenantID(tenantID))
	duration.Record(workerCtx, elapsedSeconds, metry.Labels{"status": "ok"})
	steps.Add(workerCtx, 1, metry.Labels{"status": "ok"})
	queueDepth.Record(workerCtx, queueDepthValue, metry.Labels{"queue": "jobs"})

	if err := provider.ForceFlush(ctx); err != nil {
		log.Println("flush:", err)
		return 1
	}
	fmt.Printf("exported metric families: %d\n", metricMem.Len())
	return 0
}

func registerMetrics(
	provider *metry.Provider,
) (metry.HistogramMetric, metry.CounterMetric, metry.GaugeMetric, error) {
	registry := metry.NewMetricsRegistry(provider)
	duration, err := registry.NewHistogram("agent_loop_duration", []float64{0.5, 1, 2, 4})
	if err != nil || !duration.OK() {
		if err == nil {
			err = errors.New("histogram not registered")
		}
		return metry.HistogramMetric{}, metry.CounterMetric{}, metry.GaugeMetric{}, err
	}
	steps, err := registry.NewCounter("agent_loop_steps_total")
	if err != nil || !steps.OK() {
		if err == nil {
			err = errors.New("counter not registered")
		}
		return metry.HistogramMetric{}, metry.CounterMetric{}, metry.GaugeMetric{}, err
	}
	queueDepth, err := registry.NewGauge("queue_depth")
	if err != nil || !queueDepth.OK() {
		if err == nil {
			err = errors.New("gauge not registered")
		}
		return metry.HistogramMetric{}, metry.CounterMetric{}, metry.GaugeMetric{}, err
	}
	return duration, steps, queueDepth, nil
}

func captureTraceSnapshotToken(ctx context.Context) (string, error) {
	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	if err != nil {
		return "", err
	}
	return snapshot.Marshal()
}

func restoreTraceSnapshot(ctx context.Context, provider *metry.Provider, token string) (context.Context, error) {
	snapshot, err := metry.ParseTraceSnapshot(token)
	if err != nil {
		return ctx, err
	}
	return provider.ContextWithTraceSnapshot(ctx, snapshot)
}

func main() {
	os.Exit(run())
}
