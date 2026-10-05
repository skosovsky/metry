package genai

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestConformanceTwoAdaptersSyncStreamCancelResume(t *testing.T) {
	// Arrange: independent model and tool adapters claim one owner per boundary.
	profile := DefaultProfile()
	declarations := []AdapterDeclaration{
		{
			ProfileID:  profile.ID,
			Mapping:    profile.Mapping,
			Boundaries: []string{profileModelBoundary},
			Units:      []FieldUnitDeclaration{{Field: OutputTokens, Unit: UnitTokens}},
		},
		{
			ProfileID:  profile.ID,
			Mapping:    profile.Mapping,
			Boundaries: []string{"tool"},
			Units:      nil,
		},
	}
	fixtures := make([]ConformanceFixture, 0, 4)
	for _, scenario := range []string{"sync", "stream", "cancel", "resume"} {
		fixtures = append(fixtures, func() ([]ObservedBoundary, error) {
			return lifecycleExporterFixture(t, profile, scenario), nil
		})
	}
	// Act.
	report, err := CheckConformance(testContract(profile, false), declarations, fixtures...)
	// Assert.
	require.NoError(t, err)
	assert.True(t, report.Passed(), "%+v", report.Checks)
	seen := make(map[int]bool)
	for _, check := range report.Checks {
		seen[check.Fixture] = true
	}
	assert.Len(t, seen, len(fixtures))
}

func lifecycleExporterFixture(t *testing.T, profile Profile, scenario string) []ObservedBoundary {
	t.Helper()
	tracker, provider, metricExporter, exporter := newTestTrackerWithMetrics(t,
		WithProfile(profile), WithPayloadPolicy(RedactPayloadPolicy()))
	runtime := tracker.Runtime()
	ctx := context.Background()
	options := RecordingOptions{}
	if scenario == "resume" {
		ctx = completedCheckpointContext(t, provider, runtime)
		options.Detached = true
	}
	modelCtx, model, err := runtime.StartOperation(
		ctx,
		Operation{Name: profileChatOperation, Locality: ExecutionRemote},
		options,
	)
	require.NoError(t, err)
	result := OperationResult{
		Usage: Usage{OutputTokens: Observe(2, UnitTokens, SourceProvider)},
		Payload: Payload{OutputMessages: []Message{{Role: payloadAssistantRole,
			Parts: []ContentPart{{Type: payloadTextType, Content: "private fixture response"}}}}},
	}
	if scenario == "stream" {
		require.NoError(t, model.RecordStreamingUpdate(Usage{OutputTokens: Observe(1, UnitTokens, SourceProvider)}))
		require.NoError(t, model.RecordStreamingUpdate(result.Usage))
		result.Streaming = &StreamTimings{TTFT: 0, FirstTokenPresent: true}
	}
	if scenario == "cancel" {
		result.Status, result.ErrorType = OperationStatusError, string(ErrorClassCancelled)
	}
	_ = modelCtx
	require.NoError(t, model.Finish(result))
	toolStarted := time.Now()
	_, endTool, startErr := runtime.StartTool(
		context.Background(),
		ToolCall{Name: "lookup", Arguments: `{"key":"private"}`},
	)
	require.NoError(t, startErr)
	require.NoError(t, endTool.Finish(ToolResult{Status: OperationStatusOK, Result: `{"found":true}`}))
	toolElapsed := time.Since(toolStarted)
	flushTestProvider(t, provider)
	metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
	// Assert: all scenarios account a final measurement once, including cancel.
	tokens := metrytest.FindInt64Histogram(t, metrics, metrytest.MetricSelector{Name: OutputTokenUsageMetricName})
	require.Len(t, tokens.DataPoints, 1)
	assert.Equal(t, uint64(1), tokens.DataPoints[0].Count)
	assert.Equal(t, int64(2), tokens.DataPoints[0].Sum)
	spans := exporter.GetSpans()
	if scenario == "resume" {
		require.Len(t, spans, 3)
		assert.False(t, spans[1].Parent.IsValid())
		require.Len(t, spans[1].Links, 1)
		assert.Equal(t, spans[0].SpanContext.SpanID(), spans[1].Links[0].SpanContext.SpanID())
	} else {
		require.Len(t, spans, 2)
	}
	if scenario == "cancel" {
		assert.Equal(t, string(ErrorClassCancelled), testutil.SpanStubStringAttr(t, spans[0], ErrorType))
	}
	observed := projectLifecycleWire(t, profile, spans)
	modelExpected, modelActual := projectModelAccounting(t, metrics, spans, scenario)
	toolDuration := metrytest.FindFloat64Histogram(t, metrics, metrytest.MetricSelector{Name: ToolDurationMetricName})
	require.Len(t, toolDuration.DataPoints, 1)
	toolPoint := toolDuration.DataPoints[0]
	// The scenario determines count; a separate enclosing host interval bounds
	// nondeterministic elapsed time rather than copying the recording input.
	require.Equal(t, uint64(1), toolPoint.Count)
	require.Positive(t, toolPoint.Sum)
	require.LessOrEqual(t, toolPoint.Sum, toolElapsed.Seconds())
	expectedModels := 1
	if scenario == "resume" {
		expectedModels++
	}
	actualModels, actualTools := 0, 0
	for _, item := range observed {
		if item.Boundary == "tool" {
			actualTools++
		} else {
			actualModels++
		}
	}
	for index := range observed {
		item := &observed[index]
		item.Accounting.ExpectedSpans = expectedModels
		item.Accounting.ActualSpans = actualModels
		if item.Boundary == "tool" {
			item.Accounting.ExpectedSpans, item.Accounting.ActualSpans = 1, actualTools
			item.Accounting.Expected = []AccountingMeasurement{
				{Instrument: ToolDurationMetricName, Count: 1, Total: toolPoint.Sum},
			}
			item.Accounting.Actual = []AccountingMeasurement{
				{Instrument: ToolDurationMetricName, Count: toolPoint.Count, Total: toolPoint.Sum},
			}
		}
		if observed[index].Usage.OutputTokens.Present {
			item.Accounting.Expected = slices.Clone(modelExpected)
			item.Accounting.Actual = slices.Clone(modelActual)
			item.Accounting.Expected = append(item.Accounting.Expected, AccountingMeasurement{
				Instrument: OutputTokenUsageMetricName, Count: 1, IntegerTotal: 2,
			})
			item.Accounting.Actual = append(
				item.Accounting.Actual,
				AccountingMeasurement{Instrument: OutputTokenUsageMetricName,
					Count: tokens.DataPoints[0].Count, IntegerTotal: tokens.DataPoints[0].Sum},
			)
		}
	}
	return observed
}

func projectModelAccounting(
	t *testing.T, metrics metricdata.ResourceMetrics, spans tracetest.SpanStubs, scenario string,
) ([]AccountingMeasurement, []AccountingMeasurement) {
	t.Helper()
	duration, finalDuration := 0.0, 0.0
	count := uint64(1)
	if scenario == "resume" {
		count++
	}
	for _, span := range spans {
		if testutil.SpanStubStringAttr(t, span, OperationName) == profileChatOperation {
			finalDuration = span.EndTime.Sub(span.StartTime).Seconds()
			duration += finalDuration
		}
	}
	expected := []AccountingMeasurement{{Instrument: OperationDurationMetricName, Count: count, Total: duration}}
	actual := []AccountingMeasurement{projectFloatAccounting(t, metrics, OperationDurationMetricName)}
	if scenario == "stream" {
		require.Positive(t, finalDuration)
		expected = append(
			expected,
			AccountingMeasurement{Instrument: TTFTMetricName, Count: 1, Total: 0},
			AccountingMeasurement{
				Instrument: DerivedOutputTokensPerSecondMetricName,
				Count:      1,
				Total:      2 / finalDuration,
			},
			AccountingMeasurement{Instrument: DerivedMeanOutputTokenIntervalMetricName, Count: 1, Total: finalDuration},
		)
		for _, instrument := range []string{TTFTMetricName, DerivedOutputTokensPerSecondMetricName, DerivedMeanOutputTokenIntervalMetricName} {
			actual = append(actual, projectFloatAccounting(t, metrics, instrument))
		}
	} else {
		for _, instrument := range []string{TTFTMetricName, DerivedOutputTokensPerSecondMetricName, DerivedMeanOutputTokenIntervalMetricName} {
			metrytest.AssertMetricAbsent(t, metrics, metrytest.MetricSelector{Name: instrument})
		}
	}
	return expected, actual
}

func projectFloatAccounting(t *testing.T, metrics metricdata.ResourceMetrics, instrument string) AccountingMeasurement {
	t.Helper()
	histogram := metrytest.FindFloat64Histogram(t, metrics, metrytest.MetricSelector{Name: instrument})
	result := AccountingMeasurement{Instrument: instrument}
	for _, point := range histogram.DataPoints {
		result.Count += point.Count
		result.Total += point.Sum
	}
	return result
}

func completedCheckpointContext(t *testing.T, provider *metry.Provider, runtime Runtime) context.Context {
	t.Helper()
	ctx, checkpoint, err := runtime.StartOperation(
		context.Background(),
		Operation{Name: profileChatOperation, Locality: ExecutionRemote},
		RecordingOptions{},
	)
	require.NoError(t, err)
	snapshot, err := metry.TraceSnapshotFromContext(ctx)
	require.NoError(t, err)
	token, err := snapshot.Marshal()
	require.NoError(t, err)
	require.NoError(t, checkpoint.Finish(OperationResult{}))
	parsed, err := metry.ParseTraceSnapshot(token)
	require.NoError(t, err)
	restored, err := provider.ContextWithTraceSnapshot(context.Background(), parsed)
	require.NoError(t, err)
	return restored
}

func projectLifecycleWire(t *testing.T, profile Profile, spans tracetest.SpanStubs) []ObservedBoundary {
	t.Helper()
	fields := make(map[string]ProfileField)
	for _, field := range profile.Fields {
		fields[field.Name] = field
	}
	observed := make([]ObservedBoundary, 0, len(spans))
	for _, span := range spans {
		operation := testutil.SpanStubStringAttr(t, span, OperationName)
		boundary := profileModelBoundary
		if operation == "execute_tool" {
			boundary = "tool"
		}
		item := ObservedBoundary{Boundary: boundary, Operation: operation,
			Accounting: &AccountingEvidence{ExpectedSpans: 1, ActualSpans: 1}}
		for _, attr := range span.Attributes {
			projectLifecycleAttribute(t, profile, span, attr, &item)
		}
		for _, event := range span.Events {
			assertDeclaredEvent(t, event.Name, event.Attributes)
			assertDeclaredWireFields(t, event.Attributes)
			projection := ObservedEvent{Name: event.Name, Owner: "metry.genai"}
			for _, attr := range event.Attributes {
				projection.Fields = append(projection.Fields, fields[string(attr.Key)])
				if _, controlled := testContract(profile, false).payloadChannel(string(attr.Key)); controlled {
					projection.Payloads = append(
						projection.Payloads,
						PayloadEvidence{Attribute: string(attr.Key), Value: attr.Value.AsString()},
					)
				}
			}
			item.Events = append(item.Events, projection)
		}
		assertDeclaredWireFields(t, span.Attributes)
		observed = append(observed, item)
	}
	return observed
}

func projectLifecycleAttribute(
	t *testing.T,
	profile Profile,
	span tracetest.SpanStub,
	attr attribute.KeyValue,
	item *ObservedBoundary,
) {
	t.Helper()
	field, found := declaredField(profile, string(attr.Key))
	require.True(t, found, "undeclared: %s", attr.Key)
	if field.Unit != "" {
		field.Unit = Unit(testutil.SpanStubStringAttr(t, span, measurementProvenanceKey(field.Name)+".unit"))
	}
	item.Fields = append(item.Fields, field)
	if _, controlled := testContract(profile, false).payloadChannel(field.Name); controlled {
		item.Payloads = append(item.Payloads, PayloadEvidence{Attribute: field.Name, Value: attr.Value.AsString()})
	}
	if string(attr.Key) == OutputTokens {
		require.Equal(t, attribute.INT64, attr.Value.Type())
		item.Usage.OutputTokens = Observe(int(attr.Value.AsInt64()), field.Unit,
			Source(testutil.SpanStubStringAttr(t, span, measurementProvenanceKey(OutputTokens)+".source")))
	}
	for _, enum := range profile.Enumerations() {
		if enum.Name == field.Name {
			item.Enums = append(item.Enums, ObservedEnum{Name: field.Name, Value: attr.Value.AsString()})
		}
	}
}
