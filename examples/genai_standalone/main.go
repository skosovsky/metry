package main

import (
	"context"
	"fmt"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/testutil"
)

const expectedStandaloneSpans = 3

func main() {
	ctx := context.Background()
	exporter := testutil.NewInMemoryTraceExporter()
	provider, err := metry.New(ctx, metry.WithServiceName("standalone-inference"),
		metry.WithExporter(exporter.SDKSpanExporter()))
	if err != nil {
		panic(err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()
	tracker, err := genai.NewTrackerFromProvider(provider)
	if err != nil {
		panic(err)
	}
	if err := runStandalone(ctx, tracker); err != nil {
		panic(err)
	}
	if err := provider.ForceFlush(ctx); err != nil {
		panic(err)
	}
	if exporter.Len() != expectedStandaloneSpans {
		panic("expected one model, one tool and one retrieval span")
	}
	fmt.Println("standalone model/tool/retrieval: 3 spans; no causal IDs, experiments or execution store")
}

func runStandalone(ctx context.Context, tracker *genai.Tracker) error {
	runtime := tracker.Runtime()
	_, model, err := runtime.StartOperation(ctx, genai.Operation{Name: "chat"}, genai.RecordingOptions{})
	if err != nil {
		return err
	}
	// The host performs model I/O here and supplies only its final telemetry.
	if finishErr := model.Finish(genai.OperationResult{Usage: genai.Usage{
		InputTokens: genai.Observe(0, genai.UnitTokens, genai.SourceProvider),
	}}); finishErr != nil {
		return finishErr
	}
	toolCtx, endTool := runtime.StartTool(ctx, genai.ToolCall{Name: "lookup"})
	// The host performs tool I/O here; the default profile buckets unknown names.
	runtime.RecordToolResult(toolCtx, genai.ToolResult{Result: `{"found":true}`})
	endTool()
	retrievalCtx, endRetrieval := tracker.StartRetrievalSpan(ctx, "", genai.RetrievalRequest{TopK: 1})
	// The host performs retrieval I/O; no model usage is inferred from its result.
	err = tracker.RecordRetrievalResult(retrievalCtx, genai.RetrievalResult{ReturnedChunks: 1})
	endRetrieval()
	return err
}
