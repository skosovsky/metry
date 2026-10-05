// Map-carrier propagation example for protocol adapters. Durable queues should use TraceSnapshot.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/propagation"
	"github.com/skosovsky/metry/testutil"
)

func run() int {
	ctx := context.Background()
	mem := testutil.NewInMemoryTraceExporter()
	policy, err := propagation.NewPolicy(
		propagation.PolicyConfig{IngressBaggageKeys: []string{"subject_id"}, EgressBaggageKeys: []string{"subject_id"}},
	)
	if err != nil {
		log.Println(err)
		return 1
	}
	provider, err := metry.New(ctx,
		metry.WithPropagationPolicy(policy),
		metry.WithServiceName("propagation-map-example"),
		metry.WithExporter(mem.SDKSpanExporter()),
	)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	ctx, end, err := provider.StartSpan(ctx, "client", "send")
	if err != nil {
		log.Println(err)
		return 1
	}
	ctx = metry.Enrich(ctx, metry.SubjectID("request-42"))
	headers := map[string]any{"x-request-id": "req-42"}
	provider.InjectToMap(ctx, headers)
	end()

	serverCtx := provider.ExtractFromMap(context.Background(), headers)
	_, serverEnd, err := provider.StartSpan(serverCtx, "server", "receive")
	if err != nil {
		log.Println(err)
		return 1
	}
	serverEnd()
	if err := provider.ForceFlush(ctx); err != nil {
		log.Println("flush:", err)
		return 1
	}

	fmt.Printf("exported spans: %d\n", len(mem.GetSpans()))
	fmt.Printf("headers x-request-id: %v\n", headers["x-request-id"])
	return 0
}

func main() {
	os.Exit(run())
}
