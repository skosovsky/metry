// This executable uses synthetic host results. Metry observes intervals; it does
// not run agents, retrieve documents, schedule fan-in or execute a grader.
package main

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/testutil"
)

func main() {
	ctx := context.Background()
	exporter := testutil.NewInMemoryTraceExporter()
	provider, err := metry.New(ctx, metry.WithServiceName("agent-workflow-example"),
		metry.WithExporter(exporter.SDKSpanExporter()))
	must(err)
	defer func() { _ = provider.Shutdown(context.Background()) }()
	tracker, err := genai.NewTrackerFromProvider(provider)
	must(err)
	runtime := tracker.Runtime().ForProvider("synthetic")

	var evidence observationEvidence
	observeStandaloneAndSingle(ctx, runtime, &evidence)

	// The host chooses two agents and the join. IDs below are existing host IDs.
	workflowCtx, workflow, err := runtime.StartWorkflow(ctx, genai.WorkflowInvocation{
		Name: "research", ID: "workflow-resource", Version: "v1", ConversationID: "conversation-7",
	}, genai.RecordingOptions{})
	must(err)
	evidence.workflow = trace.SpanContextFromContext(workflowCtx)
	first, firstContext := observeAgent(workflowCtx, runtime, genai.ExecutionLocal, "researcher", "agent-local")
	evidence.first = firstContext
	second, secondContext := observeAgent(
		workflowCtx, runtime, genai.ExecutionRemote, "hosted", "provider-agent-resource",
	)
	evidence.hosted = secondContext
	joinCtx, join, err := runtime.StartAgent(workflowCtx, genai.AgentInvocation{
		Name: "join", Locality: genai.ExecutionLocal,
	}, genai.RecordingOptions{Links: []metry.AsyncHandle{first, second}})
	must(err)
	evidence.join = trace.SpanContextFromContext(joinCtx)
	must(observeRetrieval(joinCtx, runtime))
	must(join.Finish(genai.InvocationResult{Status: genai.OperationStatusOK}))
	workflowReference, err := metry.NewAsyncHandle(workflowCtx)
	must(err)
	snapshot, err := metry.TraceSnapshotFromContext(workflowCtx)
	must(err)
	token, err := snapshot.Marshal()
	must(err)
	must(workflow.Finish(genai.InvocationResult{Status: genai.OperationStatusOK}))

	// Host persistence/transport restores only trace provenance, after completion.
	parsed, err := metry.ParseTraceSnapshot(token)
	must(err)
	resumed, err := provider.ContextWithTraceSnapshot(ctx, parsed)
	must(err)
	detachedCtx, detached, err := runtime.StartAgent(resumed, genai.AgentInvocation{
		Name: "resumed", Locality: genai.ExecutionLocal,
	}, genai.RecordingOptions{Detached: true})
	must(err)
	evidence.detached = trace.SpanContextFromContext(detachedCtx)
	must(detached.Finish(genai.InvocationResult{Status: genai.OperationStatusOK}))

	// The host already ran a grader; this records its ready result on a linked root.
	score := 0.0
	must(tracker.EvaluationRecorder().Record(ctx, genai.Evaluation{
		ObservationID: "evaluation-7", Metric: genai.EvaluationFaithfulness,
		Status: genai.EvaluationSucceeded, Score: &score, TraceReference: &workflowReference,
	}))
	must(provider.ForceFlush(ctx))
	verifyExports(exporter, evidence)
	fmt.Printf(
		"synthetic standalone/single-agent/workflow/fan-in/detached/retrieval/late-evaluation: %d spans\n",
		exporter.Len(),
	)
}

func observeStandaloneAndSingle(ctx context.Context, runtime genai.Runtime, evidence *observationEvidence) {
	// Model and tool calls need no agent, workflow or business identity.
	var err error
	evidence.standaloneModel, err = observeModel(ctx, runtime, genai.ExecutionRemote)
	must(err)
	evidence.standaloneTool, err = observeTool(ctx, runtime)
	must(err)

	// A single local agent owns both a local model and a tool interval.
	agentCtx, agent, err := runtime.StartAgent(ctx, genai.AgentInvocation{
		Name: "standalone", Locality: genai.ExecutionLocal,
	}, genai.RecordingOptions{})
	must(err)
	evidence.singleAgent = trace.SpanContextFromContext(agentCtx)
	evidence.singleModel, err = observeModel(agentCtx, runtime, genai.ExecutionLocal)
	must(err)
	evidence.singleTool, err = observeTool(agentCtx, runtime)
	must(err)
	must(agent.Finish(genai.InvocationResult{Status: genai.OperationStatusOK}))
}

func observeModel(
	ctx context.Context, runtime genai.Runtime, locality genai.ExecutionLocality,
) (trace.SpanContext, error) {
	modelCtx, handle, err := runtime.StartOperation(ctx, genai.Operation{
		Name: "chat", Locality: locality,
	}, genai.RecordingOptions{})
	if err != nil {
		return trace.SpanContext{}, err
	}
	defer func() { _ = handle.Close() }()
	return trace.SpanContextFromContext(modelCtx), handle.Finish(genai.OperationResult{Status: genai.OperationStatusOK,
		Usage: genai.Usage{InputTokens: genai.Observe(0, genai.UnitTokens, genai.SourceMeasured)},
	})
}

func observeAgent(
	ctx context.Context,
	runtime genai.Runtime,
	locality genai.ExecutionLocality,
	name, id string,
) (metry.AsyncHandle, trace.SpanContext) {
	childCtx, handle, err := runtime.StartAgent(ctx, genai.AgentInvocation{
		Name: name, ID: id, Version: "v1", Locality: locality,
	}, genai.RecordingOptions{})
	must(err)
	reference, err := metry.NewAsyncHandle(childCtx)
	must(err)
	must(handle.Finish(genai.InvocationResult{Status: genai.OperationStatusOK}))
	return reference, trace.SpanContextFromContext(childCtx)
}

func observeRetrieval(ctx context.Context, runtime genai.Runtime) error {
	_, handle, err := runtime.StartRetrieval(ctx, "", genai.RetrievalRequest{
		Provider: "synthetic", Source: "documents", TopK: 1, Locality: genai.ExecutionLocal,
	})
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	relevance := 0.0
	return handle.Finish(genai.RetrievalResult{
		Status:            genai.OperationStatusOK,
		ReturnedDocuments: 1,
		Documents: []genai.RetrievalDocument{
			{ID: "document-1", Version: "v2", Digest: "sha256:host-owned", Relevance: &relevance},
		},
	})
}

type observationEvidence struct {
	standaloneModel trace.SpanContext
	standaloneTool  trace.SpanContext
	singleAgent     trace.SpanContext
	singleModel     trace.SpanContext
	singleTool      trace.SpanContext
	workflow        trace.SpanContext
	first           trace.SpanContext
	hosted          trace.SpanContext
	join            trace.SpanContext
	detached        trace.SpanContext
}

func observeTool(ctx context.Context, runtime genai.Runtime) (trace.SpanContext, error) {
	toolCtx, handle, err := runtime.StartTool(ctx, genai.ToolCall{Name: "lookup"})
	if err != nil {
		return trace.SpanContext{}, err
	}
	defer func() { _ = handle.Close() }()
	// The host already obtained this synthetic result; no callback is executed.
	return trace.SpanContextFromContext(toolCtx), handle.Finish(genai.ToolResult{
		Status: genai.OperationStatusOK, Result: `{"found":true}`,
	})
}

func verifyExports(exporter *testutil.InMemoryTraceExporter, evidence observationEvidence) {
	spans := exporter.GetSpans()
	const expectedSpans = 12
	if len(spans) != expectedSpans {
		panic("missing synthetic scenario spans")
	}
	byID := make(map[trace.SpanID]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		byID[span.SpanContext.SpanID()] = span
	}
	standaloneModel := requiredSpan(byID, evidence.standaloneModel)
	standaloneTool := requiredSpan(byID, evidence.standaloneTool)
	if standaloneModel.Parent.IsValid() || standaloneTool.Parent.IsValid() ||
		standaloneModel.SpanKind != trace.SpanKindClient || standaloneTool.SpanKind != trace.SpanKindInternal {
		panic("standalone intervals must have no agent parent")
	}
	model := requiredSpan(byID, evidence.singleModel)
	tool := requiredSpan(byID, evidence.singleTool)
	if !model.Parent.Equal(evidence.singleAgent) || !tool.Parent.Equal(evidence.singleAgent) ||
		model.SpanKind != trace.SpanKindInternal || tool.SpanKind != trace.SpanKindInternal {
		panic("single agent must own both local model and tool intervals")
	}
	hosted := requiredSpan(byID, evidence.hosted)
	local := requiredSpan(byID, evidence.first)
	if !hosted.Parent.Equal(evidence.workflow) || hosted.SpanKind != trace.SpanKindClient ||
		!local.Parent.Equal(evidence.workflow) || local.SpanKind != trace.SpanKindInternal {
		panic("workflow must own the declared local and hosted agents")
	}
	join := requiredSpan(byID, evidence.join)
	if !join.Parent.Equal(evidence.workflow) || len(join.Links) != 2 ||
		!hasLink(join, evidence.first) || !hasLink(join, evidence.hosted) {
		panic("join must link both exact agent predecessors")
	}
	detached := requiredSpan(byID, evidence.detached)
	if detached.Parent.IsValid() || len(detached.Links) != 1 ||
		!hasLink(detached, evidence.workflow) || detached.SpanContext.TraceID() == evidence.workflow.TraceID() {
		panic("resumed agent must be a new root linked to the completed workflow")
	}
}

func requiredSpan(byID map[trace.SpanID]tracetest.SpanStub, reference trace.SpanContext) tracetest.SpanStub {
	span, present := byID[reference.SpanID()]
	if !reference.IsValid() || !present || !span.SpanContext.Equal(reference) {
		panic("expected exact observed interval was not exported")
	}
	return span
}

func hasLink(span tracetest.SpanStub, reference trace.SpanContext) bool {
	for _, link := range span.Links {
		if link.SpanContext.TraceID() == reference.TraceID() && link.SpanContext.SpanID() == reference.SpanID() {
			return true
		}
	}
	return false
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
