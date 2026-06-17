// Map-carrier propagation example. Full AsyncHandle queue flow: see propagation_e2e_test.go.
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
	mem := testutil.NewInMemoryTraceExporter()
	provider, err := metry.New(ctx,
		metry.WithServiceName("propagation-map-example"),
		metry.WithExporter(metrytest.MetrySpanExporter(mem)),
	)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	ctx, end, err := provider.StartSpan(ctx, "producer", "publish")
	if err != nil {
		log.Println(err)
		return 1
	}
	ctx = metry.Enrich(ctx, metry.SubjectID("job-42"))
	carrier := map[string]any{"order_id": "ord-42", "body": "payload"}
	provider.InjectToMap(ctx, carrier)
	end()

	consumerCtx := provider.ExtractFromMap(context.Background(), carrier)
	_, consumerEnd, err := provider.StartSpan(consumerCtx, "consumer", "consume")
	if err != nil {
		log.Println(err)
		return 1
	}
	consumerEnd()

	fmt.Printf("exported spans: %d\n", len(mem.GetSpans()))
	fmt.Printf("carrier order_id: %v\n", carrier["order_id"])
	return 0
}

func main() {
	os.Exit(run())
}
