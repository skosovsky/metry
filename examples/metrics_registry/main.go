package main

import (
	"context"
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

	registry := metry.NewMetricsRegistry(provider)
	duration, err := registry.NewHistogram("agent_loop_duration", []float64{0.5, 1, 2, 4})
	if err != nil {
		log.Println(err)
		return 1
	}
	if !duration.OK() {
		log.Println("histogram not registered")
		return 1
	}
	steps, err := registry.NewCounter("agent_loop_steps_total")
	if err != nil {
		log.Println(err)
		return 1
	}
	if !steps.OK() {
		log.Println("counter not registered")
		return 1
	}
	queueDepth, err := registry.NewGauge("queue_depth")
	if err != nil {
		log.Println(err)
		return 1
	}
	if !queueDepth.OK() {
		log.Println("gauge not registered")
		return 1
	}
	const elapsedSeconds = 1.2
	const queueDepthValue = 3.0

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	if err != nil {
		log.Println(err)
		return 1
	}
	ctx = metry.Enrich(ctx, metry.TenantID("t-metrics"))
	carrier := map[string]any{"job_id": "job-1"}
	provider.InjectToMap(ctx, carrier)
	end()

	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	duration.Record(workerCtx, elapsedSeconds, metry.Labels{"status": "ok"})
	steps.Add(workerCtx, 1, metry.Labels{"status": "ok"})
	queueDepth.Record(workerCtx, queueDepthValue, metry.Labels{"queue": "jobs"})

	fmt.Printf("exported metric families: %d\n", metricMem.Len())
	return 0
}

func main() {
	os.Exit(run())
}
