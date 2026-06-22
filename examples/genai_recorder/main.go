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
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

const (
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
		metry.WithExporter(metrytest.MetrySpanExporter(traceMem)),
		metry.WithMetricExporter(metrytest.MetryMetricExporter(metricMem)),
	)
	if err != nil {
		panic(err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()

	runtime := genai.RuntimeFromProvider(
		provider,
		genai.WithPayloadPolicy(genai.RedactPayloadPolicy()),
	).ForProvider("provider")

	ctx, endRequest, err := provider.StartSpan(ctx, "example", "request")
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

	err = runtime.RecordOperation(
		ctx,
		genai.Operation{
			Name:    "chat",
			Model:   "model",
			Purpose: genai.PurposeGeneration,
		},
		genai.OperationResult{
			Status:   genai.OperationStatusOK,
			Duration: demoOperationDuration,
			Usage:    genai.Usage{InputTokens: demoInputTokens, OutputTokens: demoOutputTokens},
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

	toolCtx, endTool := runtime.StartTool(ctx, genai.ToolCall{
		Name:      "search",
		CallID:    "call-1",
		Arguments: `{"query":"private"}`,
	})
	runtime.RecordToolResult(toolCtx, genai.ToolResult{Result: `{"matches":1}`})
	endTool()
	endRequest()

	parsed, err := metry.ParseTraceSnapshot(token)
	if err != nil {
		panic(err)
	}
	consumerCtx, err := provider.ContextWithTraceSnapshot(context.Background(), parsed)
	if err != nil {
		panic(err)
	}
	_, endConsumer, err := provider.StartSpan(consumerCtx, "example", "consumer")
	if err != nil {
		panic(err)
	}
	endConsumer()

	if err := provider.ForceFlush(ctx); err != nil {
		panic(err)
	}
	if !hasRuntimeOperationSpan(traceMem.GetSpans()) {
		panic("runtime operation span not exported")
	}
	rm := metricMem.LastResourceMetrics()
	if !hasFloat64Histogram(rm, genai.OperationDurationMetricName) {
		panic("operation duration metric not exported")
	}
	if !hasFloat64Histogram(rm, genai.ToolDurationMetricName) {
		panic("tool duration metric not exported")
	}

	fmt.Printf("exported spans: %d\n", traceMem.Len())
	fmt.Printf("exported operation metric: %s\n", genai.OperationDurationMetricName)
	fmt.Printf("exported tool metric: %s\n", genai.ToolDurationMetricName)
}

func hasRuntimeOperationSpan(spans tracetest.SpanStubs) bool {
	for _, span := range spans {
		if span.Name != "chat" {
			continue
		}
		if span.Status.Code.String() != "Ok" {
			return false
		}
		if attrString(span.Attributes, genai.ProviderName) != "provider" {
			return false
		}
		if attrString(span.Attributes, genai.OperationName) != "chat" {
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
