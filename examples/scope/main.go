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

	tracker, err := genai.NewTrackerFromProvider(provider)
	if err != nil {
		log.Println(err)
		return 1
	}

	scope := genai.Scope{
		Provider:  "openai",
		Model:     "gpt-4o-mini",
		Operation: "chat",
		Purpose:   genai.PurposeGeneration,
	}

	ctx, end, err := provider.StartSpan(ctx, "producer", "enqueue")
	if err != nil {
		log.Println(err)
		return 1
	}
	ctx = genai.WithScope(ctx, scope)
	ctx = metry.Enrich(ctx, metry.TenantID("t-scope"))
	carrier := map[string]any{"job_id": "scope-1"}
	provider.InjectToMap(ctx, carrier)
	end()

	workerCtx := provider.ExtractFromMap(context.Background(), carrier)
	err = tracker.RecordOperation(workerCtx, scope, func(scopedCtx context.Context) error {
		return tracker.RecordInteraction(scopedCtx, genai.Meta{}, genai.Payload{}, genai.Usage{InputTokens: 1})
	})
	if err != nil {
		log.Println(err)
		return 1
	}

	fmt.Printf("exported spans: %d\n", len(mem.GetSpans()))
	return 0
}

func main() {
	os.Exit(run())
}
