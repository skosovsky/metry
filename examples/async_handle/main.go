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
		metry.WithServiceName("async-handle-example"),
		metry.WithExporter(metrytest.MetrySpanExporter(mem)),
	)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	if err != nil {
		log.Println(err)
		return 1
	}
	ctx = metry.Enrich(ctx, metry.TenantID("t-1"))
	carrier := map[string]any{"order_id": "ord-1"}
	provider.InjectToMap(ctx, carrier)
	handle, err := metry.NewAsyncHandle(ctx)
	if err != nil {
		log.Println("capture handle:", err)
		return 1
	}
	token, err := handle.Marshal()
	if err != nil {
		log.Println("marshal handle:", err)
		return 1
	}
	end()

	parsed, err := metry.ParseAsyncHandle(token)
	if err != nil {
		log.Println("parse handle:", err)
		return 1
	}
	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	if err := parsed.RecordLinkedOutcomeWithProvider(workerCtx, provider, "delivery.success",
		metry.TenantID("t-1"),
	); err != nil {
		log.Println("record outcome:", err)
		return 1
	}

	fmt.Printf("exported spans: %d\n", len(mem.GetSpans()))
	fmt.Printf("carrier order_id: %v\n", carrier["order_id"])
	return 0
}

func main() {
	os.Exit(run())
}
