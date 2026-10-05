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
	_, model, err := runtime.StartOperation(
		ctx, genai.Operation{Name: "chat", Locality: genai.ExecutionRemote}, genai.RecordingOptions{},
	)
	if err != nil {
		return err
	}
	defer func() { _ = model.Close() }()
	// The host performs model I/O here and supplies only its final telemetry.
	if finishErr := model.Finish(genai.OperationResult{Status: genai.OperationStatusOK, Usage: genai.Usage{
		InputTokens: genai.Observe(0, genai.UnitTokens, genai.SourceProvider),
	}}); finishErr != nil {
		return finishErr
	}
	// This host-owned call preserves its business result even if instrumentation fails.
	if _, businessErr := observeLookup(ctx, runtime, func(context.Context) (string, error) {
		return `{"found":true}`, nil
	}, func(err error) { fmt.Printf("tool telemetry: %v\n", err) }); businessErr != nil {
		return businessErr
	}
	_, retrieval, err := tracker.StartRetrieval(
		ctx,
		"",
		genai.RetrievalRequest{TopK: 1, Locality: genai.ExecutionLocal},
	)
	if err != nil {
		return err
	}
	defer func() { _ = retrieval.Close() }()
	// Metry neither performs retrieval nor infers model usage from its result.
	return retrieval.Finish(genai.RetrievalResult{Status: genai.OperationStatusOK, ReturnedDocuments: 1})
}

// observeLookup controls only this application's I/O; metry owns its observation.
func observeLookup(ctx context.Context, runtime genai.Runtime,
	call func(context.Context) (string, error), reportTelemetry func(error),
) (string, error) {
	toolCtx, tool, telemetryErr := runtime.StartTool(ctx, genai.ToolCall{Name: "lookup"})
	if telemetryErr != nil {
		reportTelemetry(telemetryErr)
		toolCtx = ctx
	}
	if tool != nil {
		defer func() { _ = tool.Close() }()
	}
	result, businessErr := call(toolCtx)
	status := genai.Outcome(genai.OperationStatusOK)
	if businessErr != nil {
		status = genai.OperationStatusError
	}
	if tool != nil {
		if telemetryErr := tool.Finish(
			genai.ToolResult{Status: status, Result: result, Err: businessErr},
		); telemetryErr != nil {
			reportTelemetry(telemetryErr)
		}
	}
	return result, businessErr
}
