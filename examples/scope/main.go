package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func run() int {
	ctx := context.Background()
	mem := testutil.NewInMemoryTraceExporter()
	provider, err := metry.New(ctx,
		metry.WithServiceName("scope-example"),
		metry.WithExporter(metrytest.MetrySpanExporter(mem)),
	)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	runtime, err := genai.NewRuntimeFromProvider(provider)
	if err != nil {
		log.Println(err)
		return 1
	}

	scope := genai.Scope{
		Provider:  "",
		Model:     "gpt-4o-mini",
		Operation: "chat",
		Purpose:   genai.PurposeGeneration,
	}

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	if err != nil {
		log.Println(err)
		return 1
	}
	const tenantID = "t-scope"
	ctx = genai.WithScope(ctx, scope)
	ctx = metry.Enrich(ctx, metry.TenantID(tenantID))
	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	if err != nil {
		log.Println("capture trace snapshot:", err)
		return 1
	}
	snapshotToken, err := snapshot.Marshal()
	if err != nil {
		log.Println("marshal trace snapshot:", err)
		return 1
	}
	end()

	parsedSnapshot, err := metry.ParseTraceSnapshot(snapshotToken)
	if err != nil {
		log.Println("parse trace snapshot:", err)
		return 1
	}
	workerCtx, err := provider.ContextWithTraceSnapshot(context.Background(), parsedSnapshot)
	if err != nil {
		log.Println("restore trace snapshot:", err)
		return 1
	}
	workerCtx = genai.WithScope(workerCtx, scope)
	workerCtx = metry.Enrich(workerCtx, metry.TenantID(tenantID))
	runtime = runtime.ForProvider("openai")
	if err := runtime.RecordOperation(workerCtx, genai.Operation{
		Name:    scope.Operation,
		Model:   scope.Model,
		Purpose: scope.Purpose,
	}, genai.OperationResult{
		Status: genai.OperationStatusOK,
		Usage:  genai.Usage{InputTokens: 1},
	}); err != nil {
		log.Println(err)
		return 1
	}

	if err := provider.ForceFlush(ctx); err != nil {
		log.Println("flush:", err)
		return 1
	}
	fmt.Printf("exported spans: %d\n", len(mem.GetSpans()))
	return 0
}

func main() {
	os.Exit(run())
}
