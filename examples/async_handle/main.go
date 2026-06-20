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
	const orderID = "ord-1"
	const tenantID = "t-1"
	ctx = metry.Enrich(ctx, metry.TenantID(tenantID))
	snapshotToken, err := captureTraceSnapshotToken(ctx)
	if err != nil {
		log.Println("capture trace snapshot:", err)
		return 1
	}
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
	workerCtx, err := restoreTraceSnapshot(context.Background(), provider, snapshotToken)
	if err != nil {
		log.Println("restore trace snapshot:", err)
		return 1
	}
	if err := parsed.RecordLinkedOutcomeWithProvider(workerCtx, provider, "delivery.success",
		metry.TenantID(tenantID),
	); err != nil {
		log.Println("record outcome:", err)
		return 1
	}
	if err := provider.ForceFlush(ctx); err != nil {
		log.Println("flush:", err)
		return 1
	}

	fmt.Printf("exported spans: %d\n", len(mem.GetSpans()))
	fmt.Printf("job order_id: %s\n", orderID)
	return 0
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
