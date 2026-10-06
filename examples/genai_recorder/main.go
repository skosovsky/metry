package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/testutil"
)

const (
	demoOperation         = "chat"
	demoOperationDuration = 25 * time.Millisecond
	demoInputTokens       = 12
	demoOutputTokens      = 6
)

func main() {
	ctx := context.Background()
	traceMem := testutil.NewInMemoryTraceExporter()
	metricMem := testutil.NewInMemoryMetricExporter()
	provider, err := metry.New(ctx,
		metry.WithServiceName("genai-recorder-example"),
		metry.WithExporter(traceMem.SDKSpanExporter()),
		metry.WithMetricExporter(metricMem.SDKExporter()),
	)
	if err != nil {
		panic(err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()

	recorder, evaluations, err := exampleRecorders(provider)
	if err != nil {
		panic(err)
	}

	ctx, endRequest, err := provider.StartSpan(ctx, "example", "request")
	if err != nil {
		panic(err)
	}
	origin, err := metry.NewAsyncHandle(ctx)
	if err != nil {
		panic(err)
	}
	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	if err != nil {
		panic(err)
	}
	token, err := snapshot.Marshal()
	if err != nil {
		panic(err)
	}

	err = recorder.RecordOperation(
		ctx,
		genai.Operation{
			Name:    demoOperation,
			Model:   "model",
			Purpose: genai.PurposeGeneration,
		},
		genai.OperationResult{
			Status:   genai.OperationStatusOK,
			Duration: demoOperationDuration,
			Usage: genai.Usage{
				InputTokens:  genai.Observe(demoInputTokens, genai.UnitTokens, genai.SourceMeasured),
				OutputTokens: genai.Observe(demoOutputTokens, genai.UnitTokens, genai.SourceMeasured),
			},
			Payload: genai.Payload{
				InputMessages: []genai.Message{{
					Role:  "user",
					Parts: []genai.ContentPart{{Type: "text", Content: "summarize private data"}},
				}},
			},
		},
		metry.StringAttribute("example.outcome", "ok"),
	)
	if err != nil {
		panic(err)
	}

	if err := recordExampleTool(ctx, recorder); err != nil {
		panic(err)
	}
	endRequest()

	if err := resumeExampleConsumer(provider, token); err != nil {
		panic(err)
	}

	// The host runs its own grader. Metry only records the completed result.
	if err := recordExampleEvaluation(evaluations, origin); err != nil {
		panic(err)
	}

	if err := provider.ForceFlush(ctx); err != nil {
		panic(err)
	}
	if !hasRecorderOperationSpan(traceMem.GetSpans()) {
		panic("recorder operation span not exported")
	}
	rm := metricMem.LastResourceMetrics()
	if !hasFloat64Histogram(rm, genai.ObservedOperationDurationMetricName) {
		panic("operation duration metric not exported")
	}
	if !hasFloat64Histogram(rm, genai.ToolDurationMetricName) {
		panic("tool duration metric not exported")
	}

	fmt.Printf("exported spans: %d\n", traceMem.Len())
	fmt.Printf("exported operation metric: %s\n", genai.ObservedOperationDurationMetricName)
	fmt.Printf("exported tool metric: %s\n", genai.ToolDurationMetricName)
}

func exampleRecorders(provider *metry.Provider) (genai.Recorder, *genai.EvaluationRecorder, error) {
	profile := genai.DefaultProfile()
	for index := range profile.Dimensions {
		switch profile.Dimensions[index].Key {
		case genai.ProviderName:
			profile.Dimensions[index].Values = []string{"provider"}
		case genai.RequestModel, genai.ResponseModel:
			profile.Dimensions[index].Values = []string{"model"}
		case genai.ToolName:
			profile.Dimensions[index].Values = []string{"search"}
		}
	}
	tracker, err := genai.NewTrackerFromProvider(
		provider,
		genai.WithProfile(profile),
		genai.WithPayloadPolicy(genai.RedactPayloadPolicy()),
	)
	if err != nil {
		return nil, nil, err
	}
	return tracker.Recorder().ForProvider("provider"), tracker.EvaluationRecorder(), nil
}

func resumeExampleConsumer(provider *metry.Provider, token string) error {
	parsed, err := metry.ParseTraceSnapshot(token)
	if err != nil {
		return err
	}
	consumerCtx, err := provider.ContextWithTraceSnapshot(context.Background(), parsed)
	if err != nil {
		return err
	}
	_, endConsumer, err := provider.StartSpan(consumerCtx, "example", "consumer")
	if err != nil {
		return err
	}
	endConsumer()
	return nil
}

func recordExampleEvaluation(recorder *genai.EvaluationRecorder, origin metry.AsyncHandle) error {
	return recorder.Record(context.Background(), genai.Evaluation{
		ObservationID:  "evaluation-one",
		Metric:         genai.EvaluationFaithfulness,
		Status:         genai.EvaluationSucceeded,
		Outcome:        "pass",
		TraceReference: &origin,
	})
}

func hasRecorderOperationSpan(spans tracetest.SpanStubs) bool {
	for _, span := range spans {
		if span.Name != demoOperation+" model" {
			continue
		}
		if span.Status.Code.String() != "Ok" {
			return false
		}
		if attrString(span.Attributes, genai.ProviderName) != "provider" {
			return false
		}
		if attrString(span.Attributes, genai.OperationName) != demoOperation {
			return false
		}
		if attrString(span.Attributes, genai.OperationStatus) != genai.OperationStatusOK {
			return false
		}
		inputMessages := attrString(span.Attributes, genai.InputMessages)
		if !strings.Contains(inputMessages, "[redacted]") || strings.Contains(inputMessages, "private") {
			return false
		}
		return true
	}
	return false
}

func hasFloat64Histogram(rm *metricdata.ResourceMetrics, name string) bool {
	if rm == nil {
		return false
	}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				hist, ok := metric.Data.(metricdata.Histogram[float64])
				return ok && len(hist.DataPoints) > 0
			}
		}
	}
	return false
}

func attrString(attrs []attribute.KeyValue, key string) string {
	set := attribute.NewSet(attrs...)
	value, ok := set.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return value.AsString()
}

func recordExampleTool(ctx context.Context, recorder genai.Recorder) error {
	_, tool, err := recorder.StartTool(ctx, genai.ToolCall{
		Name: "search", CallID: "call-1", Arguments: `{"query":"private"}`,
	})
	if err != nil {
		return err
	}
	defer func() { _ = tool.Close() }()
	return tool.Finish(genai.ToolResult{Status: genai.OperationStatusOK, Result: `{"matches":1}`})
}
