package genai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const oracleScopeVersion = "semconv-genai-e07f4eb+metry-v1"
const oracleSchemaURL = "https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev"

func TestPinnedOracleFixtureIntegrity(t *testing.T) {
	// Arrange: immutable selected upstream files, independent of local Profile.
	var pin struct {
		Commit    string            `json:"commit"`
		SchemaURL string            `json:"schema_url"`
		SHA256    map[string]string `json:"sha256"`
	}
	data, err := os.ReadFile("testdata/semconv/PIN.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &pin))
	// Act and Assert.
	require.Equal(t, "e07f4ebacb08f56db8c4c882d117720333fbca04", pin.Commit)
	require.Equal(t, oracleSchemaURL, pin.SchemaURL)
	require.Len(t, pin.SHA256, 13)
	for name, want := range pin.SHA256 {
		data, err = os.ReadFile(filepath.Join("testdata/semconv", name))
		require.NoError(t, err)
		hash := sha256.Sum256(data)
		require.Equal(t, want, hex.EncodeToString(hash[:]), name)
	}
}

func oracleSchema(t *testing.T, name, fragment string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile("testdata/semconv/model/gen-ai/gen-ai-" + name + ".json" + fragment)
	require.NoError(t, err)
	return schema
}

func oracleJSON(t *testing.T, text string) any {
	t.Helper()
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(text))
	require.NoError(t, err)
	return value
}

func TestPinnedOracleSchemasAndRecognizedVocabularyMutations(t *testing.T) {
	// Arrange: GenericPart deliberately permits arbitrary types. Known vocabularies
	// therefore also require their pinned branch, not merely the enclosing anyOf.
	cases := []struct{ name, fragment, valid, invalid string }{
		{
			"input-messages",
			"",
			`[{"role":"user","parts":[{"type":"text","content":"hello"}]}]`,
			`{"data":[],"truncated":true}`,
		},
		{
			"output-messages",
			"",
			`[{"role":"assistant","parts":[{"type":"tool_call_response","response":{"ok":true}}]}]`,
			`[{"parts":[]}]`,
		},
		{"system-instructions", "", `[{"type":"text","content":"hello"}]`, `{"data":[],"truncated":true}`},
		{"retrieval-documents", "", `[{"id":"doc","score":0}]`, `[{"score":"0"}]`},
		{"tool-call-arguments", "", `{"x":1}`, `[1]`},
		{"tool-call-result", "", `{"x":1}`, `true`},
		{"input-messages", "#/$defs/TextPart", `{"type":"text","content":"hello"}`, `{"type":"text","text":"hello"}`},
		{
			"input-messages",
			"#/$defs/ToolCallRequestPart",
			`{"type":"tool_call","name":"search","arguments":null}`,
			`{"type":"tool_call","arguments":null}`,
		},
		{
			"input-messages",
			"#/$defs/ToolCallResponsePart",
			`{"type":"tool_call_response","response":null}`,
			`{"type":"tool_result","result":null}`,
		},
	}
	// Act and Assert: each negative fixture must be rejected by actual upstream schema.
	for _, tc := range cases {
		t.Run(tc.name+tc.fragment, func(t *testing.T) {
			schema := oracleSchema(t, tc.name, tc.fragment)
			require.NoError(t, schema.Validate(oracleJSON(t, tc.valid)))
			require.Error(t, schema.Validate(oracleJSON(t, tc.invalid)))
		})
	}
}

func oracleAttrs(attrs []attribute.KeyValue) map[string]attribute.Value {
	values := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value
	}
	return values
}

func oracleSpan(spans tracetest.SpanStubs, name string) (tracetest.SpanStub, error) {
	for _, span := range spans {
		if span.Name == name {
			return span, nil
		}
	}
	return tracetest.SpanStub{}, fmt.Errorf("missing span %s", name)
}

func assertOracleSpan(t *testing.T, spans tracetest.SpanStubs, name string, kind trace.SpanKind) tracetest.SpanStub {
	t.Helper()
	span, err := oracleSpan(spans, name)
	require.NoError(t, err)
	require.Equal(t, kind, span.SpanKind)
	require.Equal(t, oracleScopeVersion, span.InstrumentationScope.Version)
	require.Equal(t, oracleSchemaURL, span.InstrumentationScope.SchemaURL)
	require.NotEqual(t, oracleSchemaURL, span.Resource.SchemaURL())
	require.Equal(t, "https://opentelemetry.io/schemas/1.43.0", span.Resource.SchemaURL())
	return span
}

func TestPinnedOracleActualExports(t *testing.T) {
	// Arrange: literals below come from the selected registry/metrics and final
	// explicit extension contract, never current constants/Profile declarations.
	tracker, provider, metrics, exporter := newTestTrackerWithMetrics(t, WithRawPayloads())
	recorder := tracker.Recorder().ForProvider("openai")
	ctx := context.Background()
	usage := Usage{
		InputTokens:           Observe(11, UnitTokens, SourceProvider),
		OutputTokens:          Observe(7, UnitTokens, SourceProvider),
		CacheWriteInputTokens: Observe(3, UnitTokens, SourceProvider),
	}
	payload := Payload{
		SystemInstructions: []ContentPart{{Type: "text", Content: "system"}},
		InputMessages:      []Message{{Role: "user", Parts: []ContentPart{{Type: "text", Content: "hello"}}}},
		OutputMessages: []Message{
			{
				Role: "assistant",
				Parts: []ContentPart{
					{Type: "tool_call", Name: "search", Arguments: json.RawMessage(`{"q":1}`)},
					{Type: "tool_call_response", Response: json.RawMessage(`null`)},
				},
			},
		},
	}
	// Act: real model boundaries, completed model observation, universal tool JSON,
	// retrieval known-zero score, hosted/local agents and a workflow.
	_, remote, err := recorder.StartOperation(
		ctx,
		Operation{Name: "chat", Model: "model", Locality: ExecutionRemote},
		RecordingOptions{},
	)
	require.NoError(t, err)
	require.NoError(t, remote.Finish(OperationResult{Status: OperationStatusOK, Usage: usage, Payload: payload}))
	_, local, err := recorder.StartOperation(
		ctx,
		Operation{Name: "chat", Model: "model", Locality: ExecutionLocal},
		RecordingOptions{},
	)
	require.NoError(t, err)
	require.NoError(t, local.Close())
	require.NoError(
		t,
		recorder.RecordOperation(
			ctx,
			Operation{Name: "chat", Model: "model"},
			OperationResult{Status: OperationStatusOK},
		),
	)
	_, tool, err := recorder.StartTool(ctx, ToolCall{Name: "search", Arguments: `[1,true,null]`})
	require.NoError(t, err)
	require.NoError(t, tool.Finish(ToolResult{Status: OperationStatusOK, Result: `42`}))
	_, retrieval, err := tracker.StartRetrieval(
		ctx,
		"caller-name",
		RetrievalRequest{Source: "documents", Provider: "qdrant", Locality: ExecutionRemote, TopK: 3},
	)
	require.NoError(t, err)
	zero := 0.0
	require.NoError(
		t,
		retrieval.Finish(
			RetrievalResult{
				Status:            OperationStatusOK,
				ReturnedDocuments: 3,
				Documents:         []RetrievalDocument{{ID: "doc", Version: "v1", Digest: "digest", Relevance: &zero}},
			},
		),
	)
	for _, dto := range []AgentInvocation{{Name: "local-agent", Locality: ExecutionLocal, ID: "agent"}, {Name: "hosted-agent", Locality: ExecutionRemote, ID: "hosted"}} {
		_, handle, invokeErr := tracker.StartAgent(ctx, dto, RecordingOptions{})
		require.NoError(t, invokeErr)
		require.NoError(t, handle.Finish(InvocationResult{Status: OperationStatusOK}))
	}
	_, workflow, err := tracker.StartWorkflow(ctx, WorkflowInvocation{Name: "pipeline"}, RecordingOptions{})
	require.NoError(t, err)
	require.NoError(t, workflow.Close())
	score := 0.5
	require.NoError(
		t,
		tracker.EvaluationRecorder().
			Record(ctx, Evaluation{ObservationID: "eval", Metric: EvaluationFaithfulness, Status: EvaluationSucceeded, Score: &score, Reasoning: "evidence"}),
	)
	require.NoError(
		t,
		tracker.EvaluationRecorder().
			Record(ctx, Evaluation{ObservationID: "label-eval", Metric: EvaluationFaithfulness, Status: EvaluationSucceeded, Outcome: "pass"}),
	)
	flushTestProvider(t, provider)
	// Assert: names and kinds do not derive from implementation mappings.
	spans := exporter.GetSpans()
	require.Len(t, spans, 10)
	assertOracleModelSpans(t, spans)
	assertOracleOtherSpans(t, spans)
	assertOracleEvaluationSpans(t, spans)
	assertOracleMetricExports(t, metrics.LastResourceMetrics())
}

// checkOracleModel deliberately compares literals and SDK types, independently
// of Profile and Conformance. Its mutation tests prevent a self-confirming oracle.
func checkOracleModel(span tracetest.SpanStub) error {
	if span.Name != "chat model" || span.SpanKind != trace.SpanKindClient {
		return errors.New("model name/kind mismatch")
	}
	attrs := oracleAttrs(span.Attributes)
	for _, key := range []string{"gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens", "gen_ai.usage.cache_write.input_tokens"} {
		value, ok := attrs[key]
		if !ok || value.Type() != attribute.INT64 {
			return fmt.Errorf("model attribute mismatch: %s", key)
		}
	}
	if _, old := attrs["gen_ai.usage.cache_creation.input_tokens"]; old {
		return errors.New("legacy attribute")
	}
	return nil
}

func checkOracleInputMetric(metric metricdata.Metrics) error {
	if metric.Name != "gen_ai.client.inference.operation.input_tokens" || metric.Unit != "{token}" {
		return errors.New("metric name/unit mismatch")
	}
	if _, ok := metric.Data.(metricdata.Histogram[int64]); !ok {
		return errors.New("metric aggregation mismatch")
	}
	return nil
}

func TestPinnedOracleActualTruncatedArrays(t *testing.T) {
	// Arrange: one complete small element followed by one exceeding the channel cap.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads(), WithPayloadByteLimit(128))
	payload := Payload{
		SystemInstructions: []ContentPart{
			{Type: "text", Content: "small"},
			{Type: "text", Content: strings.Repeat("large", 100)},
		},
		InputMessages: []Message{
			{Role: "user", Parts: []ContentPart{{Type: "text", Content: "small"}}},
			{Role: "user", Parts: []ContentPart{{Type: "text", Content: strings.Repeat("large", 100)}}},
		},
		OutputMessages: []Message{
			{Role: "assistant", Parts: []ContentPart{{Type: "text", Content: "small"}}},
			{Role: "assistant", Parts: []ContentPart{{Type: "text", Content: strings.Repeat("large", 100)}}},
		},
	}
	// Act.
	require.NoError(
		t,
		tracker.RecordInteraction(
			context.Background(),
			Meta{Operation: "chat", RequestModel: "model"},
			payload,
			Usage{},
		),
	)
	flushTestProvider(t, provider)
	// Assert: real exports remain upstream arrays with a complete element prefix.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	attrs := oracleAttrs(spans[0].Attributes)
	for _, channel := range []struct{ key, schema, marker string }{{"gen_ai.system_instructions", "system-instructions", "metry.gen_ai.payload.system_instructions.truncated"}, {"gen_ai.input.messages", "input-messages", "metry.gen_ai.payload.input_messages.truncated"}, {"gen_ai.output.messages", "output-messages", "metry.gen_ai.payload.output_messages.truncated"}} {
		value := attrs[channel.key].AsString()
		require.LessOrEqual(t, len(value), 128)
		require.NoError(t, oracleSchema(t, channel.schema, "").Validate(oracleJSON(t, value)))
		var elements []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(value), &elements))
		require.Len(t, elements, 1)
		require.Equal(t, attribute.BOOL, attrs[channel.marker].Type())
		require.True(t, attrs[channel.marker].AsBool())
	}
}

func assertOracleModelSpans(t *testing.T, spans tracetest.SpanStubs) {
	t.Helper()
	internalModels, completedModels := 0, 0
	for _, span := range spans {
		if span.Name == "chat model" && span.SpanKind == trace.SpanKindInternal {
			internalModels++
			if oracleAttrs(span.Attributes)["metry.observation.kind"].AsString() == "completed" {
				completedModels++
			}
		}
	}
	require.Equal(t, 2, internalModels)
	require.Equal(t, 1, completedModels)
	model := assertOracleSpan(t, spans, "chat model", trace.SpanKindClient)
	require.NoError(t, checkOracleModel(model))
	assertOracleModelMutations(t, model)
	attrs := oracleAttrs(model.Attributes)
	require.Equal(t, int64(3), attrs["gen_ai.usage.cache_write.input_tokens"].AsInt64())
	require.Equal(t, attribute.INT64, attrs["gen_ai.usage.input_tokens"].Type())
	for _, channel := range []struct{ key, schema string }{{"gen_ai.system_instructions", "system-instructions"}, {"gen_ai.input.messages", "input-messages"}, {"gen_ai.output.messages", "output-messages"}} {
		value, ok := attrs[channel.key]
		require.True(t, ok, channel.key)
		require.NoError(t, oracleSchema(t, channel.schema, "").Validate(oracleJSON(t, value.AsString())))
	}
	var inputMessages []struct {
		Parts []json.RawMessage `json:"parts"`
	}
	require.NoError(t, json.Unmarshal([]byte(attrs["gen_ai.input.messages"].AsString()), &inputMessages))
	require.Len(t, inputMessages, 1)
	require.Len(t, inputMessages[0].Parts, 1)
	require.NoError(
		t,
		oracleSchema(
			t,
			"input-messages",
			"#/$defs/TextPart",
		).Validate(oracleJSON(t, string(inputMessages[0].Parts[0]))),
	)
	var instructions []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(attrs["gen_ai.system_instructions"].AsString()), &instructions))
	require.Len(t, instructions, 1)
	require.NoError(
		t,
		oracleSchema(t, "system-instructions", "#/$defs/TextPart").Validate(oracleJSON(t, string(instructions[0]))),
	)
	// Root arrays alone are insufficient: validate each recognized content branch.
	var messages []struct {
		Parts []json.RawMessage `json:"parts"`
	}
	require.NoError(t, json.Unmarshal([]byte(attrs["gen_ai.output.messages"].AsString()), &messages))
	require.Len(t, messages, 1)
	require.Len(t, messages[0].Parts, 2)
	require.NoError(
		t,
		oracleSchema(
			t,
			"output-messages",
			"#/$defs/ToolCallRequestPart",
		).Validate(oracleJSON(t, string(messages[0].Parts[0]))),
	)
	require.NoError(
		t,
		oracleSchema(
			t,
			"output-messages",
			"#/$defs/ToolCallResponsePart",
		).Validate(oracleJSON(t, string(messages[0].Parts[1]))),
	)
}

func assertOracleModelMutations(t *testing.T, model tracetest.SpanStub) {
	t.Helper()
	t.Run("actual-span-mutations", func(t *testing.T) {
		for _, mutate := range []func(*tracetest.SpanStub){func(s *tracetest.SpanStub) { s.Name = "chat" }, func(s *tracetest.SpanStub) { s.SpanKind = trace.SpanKindInternal }, func(s *tracetest.SpanStub) {
			for i, a := range s.Attributes {
				if string(a.Key) == "gen_ai.usage.input_tokens" {
					s.Attributes[i] = attribute.String("gen_ai.usage.input_tokens", "11")
				}
			}
		}, func(s *tracetest.SpanStub) {
			for i, a := range s.Attributes {
				if string(a.Key) == "gen_ai.usage.cache_write.input_tokens" {
					s.Attributes[i] = attribute.Int64("gen_ai.usage.cache_creation.input_tokens", 3)
				}
			}
		}} {
			mutated := model
			mutated.Attributes = append([]attribute.KeyValue(nil), model.Attributes...)
			mutate(&mutated)
			require.Error(t, checkOracleModel(mutated))
		}
	})
}

func assertOracleOtherSpans(t *testing.T, spans tracetest.SpanStubs) {
	t.Helper()
	toolSpan := assertOracleSpan(t, spans, "execute_tool search", trace.SpanKindInternal)
	toolAttrs := oracleAttrs(toolSpan.Attributes)
	require.Equal(t, `[1,true,null]`, toolAttrs["metry.gen_ai.tool.call.arguments"].AsString())
	require.Equal(t, `42`, toolAttrs["metry.gen_ai.tool.call.result"].AsString())
	require.NotContains(t, toolAttrs, "gen_ai.tool.call.arguments")
	retrieved := assertOracleSpan(t, spans, "retrieval documents", trace.SpanKindClient)
	docAttrs := oracleAttrs(retrieved.Attributes)
	require.NoError(
		t,
		oracleSchema(
			t,
			"retrieval-documents",
			"",
		).Validate(oracleJSON(t, docAttrs["gen_ai.retrieval.documents"].AsString())),
	)
	require.Equal(t, int64(3), docAttrs["metry.gen_ai.retrieval.returned_documents"].AsInt64())
	require.Equal(t, "documents", docAttrs["gen_ai.data_source.id"].AsString())
	var documents []map[string]any
	require.NoError(t, json.Unmarshal([]byte(docAttrs["gen_ai.retrieval.documents"].AsString()), &documents))
	require.Len(t, documents, 1)
	require.Contains(t, documents[0], "score")
	score, ok := documents[0]["score"].(float64)
	require.True(t, ok)
	require.InDelta(t, 0.0, score, 0.0)
	require.Equal(t, "v1", documents[0]["metry.document.version"])
	require.Equal(t, "digest", documents[0]["metry.document.digest"])
	assertOracleSpan(t, spans, "invoke_agent local-agent", trace.SpanKindInternal)
	hosted := assertOracleSpan(t, spans, "invoke_agent hosted-agent", trace.SpanKindClient)
	require.Equal(t, "hosted", oracleAttrs(hosted.Attributes)["gen_ai.agent.id"].AsString())
	assertOracleSpan(t, spans, "invoke_workflow pipeline", trace.SpanKindInternal)
}

func assertOracleEvaluationSpans(t *testing.T, spans tracetest.SpanStubs) {
	t.Helper()
	foundEval := false
	for _, span := range spans {
		for _, event := range span.Events {
			if event.Name == "gen_ai.evaluation.result" {
				foundEval = true
				eventAttrs := oracleAttrs(event.Attributes)
				require.Equal(t, "faithfulness", eventAttrs["gen_ai.evaluation.name"].AsString())
				if label, ok := eventAttrs["gen_ai.evaluation.score.label"]; ok {
					require.Equal(t, attribute.STRING, label.Type())
					require.Equal(t, "pass", label.AsString())
					continue
				}
				require.Equal(t, attribute.FLOAT64, eventAttrs["gen_ai.evaluation.score.value"].Type())
				require.InDelta(t, 0.5, eventAttrs["gen_ai.evaluation.score.value"].AsFloat64(), 0.0)
				require.Equal(t, attribute.STRING, eventAttrs["gen_ai.evaluation.explanation"].Type())
				require.Equal(t, "evidence", eventAttrs["gen_ai.evaluation.explanation"].AsString())
			}
		}
	}
	require.True(t, foundEval)
}

func assertOracleMetricExports(t *testing.T, rm *metricdata.ResourceMetrics) {
	t.Helper()
	require.NotNil(t, rm)
	require.NotEqual(t, oracleSchemaURL, rm.Resource.SchemaURL())
	require.Equal(t, "https://opentelemetry.io/schemas/1.43.0", rm.Resource.SchemaURL())
	want := map[string]string{
		"gen_ai.client.inference.operation.input_tokens":  "{token}",
		"gen_ai.client.inference.operation.output_tokens": "{token}",
		"gen_ai.client.operation.duration":                "s",
		"metry.gen_ai.operation.duration":                 "s",
		"gen_ai.execute_tool.duration":                    "s",
		"metry.gen_ai.invocation.duration":                "s",
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			unit, ok := want[m.Name]
			if !ok {
				continue
			}
			require.Equal(t, unit, m.Unit, m.Name)
			require.Equal(t, oracleScopeVersion, sm.Scope.Version)
			require.Equal(t, oracleSchemaURL, sm.Scope.SchemaURL)
			switch m.Name {
			case "gen_ai.client.inference.operation.input_tokens", "gen_ai.client.inference.operation.output_tokens":
				require.IsType(t, metricdata.Histogram[int64]{}, m.Data)
			default:
				require.IsType(t, metricdata.Histogram[float64]{}, m.Data)
			}
			if m.Name == "gen_ai.client.inference.operation.input_tokens" {
				require.NoError(t, checkOracleInputMetric(m))
				for _, mutate := range []func(*metricdata.Metrics){func(m *metricdata.Metrics) { m.Name = "gen_ai.client.token.usage" }, func(m *metricdata.Metrics) { m.Unit = "token" }, func(m *metricdata.Metrics) { m.Data = metricdata.Sum[int64]{} }} {
					mutated := m
					mutate(&mutated)
					require.Error(t, checkOracleInputMetric(mutated))
				}
			}
			delete(want, m.Name)
		}
	}
	require.Empty(t, want)
}
