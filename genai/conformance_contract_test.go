package genai

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestConformanceUsesExportedEffectiveJSONBudgets(t *testing.T) {
	for _, budget := range []int{len(truncatedJSONMarker), 64, 65536, 100000} {
		for _, channel := range []string{SystemInstructions, InputMessages, OutputMessages, ToolCallArguments, ToolCallResult} {
			t.Run(channel+"/"+stringBudget(budget), func(t *testing.T) {
				// Arrange: input is independent of the exporter-derived evidence below.
				tracker, provider, exporter := newTestTracker(t, WithRawPayloads(), WithPayloadByteLimit(budget))
				content := strings.Repeat("x", 70000)
				boundary, operation := profileModelBoundary, profileChatOperation
				// Act.
				if channel == ToolCallArguments || channel == ToolCallResult {
					boundary, operation = "tool", profileToolOperation
					_, h, err := tracker.Recorder().
						StartTool(context.Background(), ToolCall{Name: "search", Arguments: `{"value":"` + content + `"}`})
					require.NoError(t, err)
					require.NoError(
						t,
						h.Finish(ToolResult{Status: OperationStatusOK, Result: `{"value":"` + content + `"}`}),
					)
				} else {
					parts := []ContentPart{{Type: "text", Content: content}}
					payload := Payload{
						SystemInstructions: parts,
						InputMessages:      []Message{{Role: "user", Parts: parts}},
						OutputMessages:     []Message{{Role: "assistant", Parts: parts}},
					}
					require.NoError(t, tracker.RecordInteraction(context.Background(), testMeta(), payload, Usage{}))
				}
				flushTestProvider(t, provider)
				spans := exporter.GetSpans()
				require.Len(t, spans, 1)
				value := testutil.SpanStubStringAttr(t, spans[0], channel)
				contract := tracker.Contract()
				operationField, _ := declaredField(OperationName)
				payloadField, _ := declaredField(channel)
				observed := ObservedBoundary{
					Boundary:   boundary,
					Operation:  operation,
					Fields:     []ProfileField{operationField, payloadField},
					Payloads:   []PayloadEvidence{{Attribute: channel, Value: value}},
					Accounting: &AccountingEvidence{ExpectedSpans: 1, ActualSpans: 1},
				}
				declarations := []AdapterDeclaration{
					{ProfileID: Schema().ID(), Mapping: Schema().Mapping(), Boundaries: []string{boundary}},
				}
				fixture := func() ([]ObservedBoundary, error) { return []ObservedBoundary{observed}, nil }
				report, err := CheckConformance(contract, declarations, fixture)
				// Assert: actual export fits its own contract, not a global default.
				require.NoError(t, err)
				require.True(t, report.Passed(), "%+v", report.Checks)
				require.LessOrEqual(t, len(value), budget)
				if budget == 100000 {
					require.Greater(t, len(value), 65536)
				}
				smaller, err := NewContract(WithRawPayloads(), WithPayloadByteLimit(len(truncatedJSONMarker)))
				require.NoError(t, err)
				rejected, err := CheckConformance(smaller, declarations, fixture)
				require.NoError(t, err)
				require.Equal(t, len(value) <= len(truncatedJSONMarker), rejected.Passed())
			})
		}
	}
}

func stringBudget(value int) string { return strconv.Itoa(value) }

func TestConformanceEventPayloadRequiresActualEvidenceAndLocalBudget(t *testing.T) {
	for _, scenario := range []string{"valid", "oversize", "missing", "duplicate", "foreign"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange.
			contract, err := NewContract(WithEventTextByteLimit(3))
			require.NoError(t, err)
			observed := zeroAccountingObservation()
			field, _ := declaredField(EvaluationExplanation)
			event := ObservedEvent{
				Name:     "gen_ai.evaluation.result",
				Owner:    "metry.genai",
				Fields:   []ProfileField{field},
				Payloads: []PayloadEvidence{{Attribute: EvaluationExplanation, Value: "abc"}},
			}
			switch scenario {
			case "oversize":
				event.Payloads[0].Value = "abcd"
			case "missing":
				event.Payloads = nil
			case "duplicate":
				event.Payloads = append(event.Payloads, event.Payloads[0])
			case "foreign":
				event.Payloads[0].Attribute = RetrievalQuery
			}
			observed.Events = []ObservedEvent{event}
			// Act.
			report, err := CheckConformance(
				contract,
				[]AdapterDeclaration{
					{ProfileID: Schema().ID(), Mapping: Schema().Mapping(), Boundaries: []string{profileModelBoundary}},
				},
				func() ([]ObservedBoundary, error) { return []ObservedBoundary{observed}, nil },
			)
			// Assert.
			require.NoError(t, err)
			require.Equal(t, scenario == "valid", report.Passed(), "%+v", report.Checks)
		})
	}
}

func TestConformanceExporterDerivedEvaluationLabelBudget(t *testing.T) {
	// Arrange: host scenario text is not used as exported evidence.
	tracker, provider, exporter := newTestTracker(t, WithRawPayloads(), WithEventTextByteLimit(17))
	evaluation := successfulEvaluation()
	evaluation.Reasoning = strings.Repeat("reason", 100)
	// Act.
	require.NoError(t, tracker.EvaluationRecorder().Record(context.Background(), evaluation))
	flushTestProvider(t, provider)
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	contract := tracker.Contract()
	profile := contract.Profile()
	observed := ObservedBoundary{
		Boundary:   profileEvaluationOperation,
		Operation:  profileEvaluationOperation,
		Accounting: &AccountingEvidence{ExpectedSpans: 1, ActualSpans: 1},
	}
	for _, attr := range spans[0].Attributes {
		field, found := declaredField(string(attr.Key))
		require.True(t, found)
		observed.Fields = append(observed.Fields, field)
		if _, controlled := contract.payloadChannel(field.Name); controlled {
			observed.Payloads = append(
				observed.Payloads,
				PayloadEvidence{Attribute: field.Name, Value: attr.Value.AsString()},
			)
		}
		for _, enum := range effectiveEnumerations(profile) {
			if enum.Name == field.Name {
				observed.Enums = append(observed.Enums, ObservedEnum{Name: field.Name, Value: attr.Value.AsString()})
			}
		}
	}
	declarations := []AdapterDeclaration{
		{ProfileID: Schema().ID(), Mapping: Schema().Mapping(), Boundaries: []string{profileEvaluationOperation}},
	}
	fixture := func() ([]ObservedBoundary, error) { return []ObservedBoundary{observed}, nil }
	report, err := CheckConformance(contract, declarations, fixture)
	// Assert.
	require.NoError(t, err)
	require.True(t, report.Passed(), "%+v", report.Checks)
	small, err := NewContract(WithRawPayloads(), WithEventTextByteLimit(1))
	require.NoError(t, err)
	report, err = CheckConformance(small, declarations, fixture)
	require.NoError(t, err)
	require.False(t, report.Passed())
}

func TestConformanceJSONEvidenceEnforcesStructuralNodeBudget(t *testing.T) {
	for _, channel := range []string{ToolCallArguments, ToolCallResult, InputMessages, OutputMessages, SystemInstructions} {
		for _, envelope := range []bool{false, true} {
			t.Run(channel+"/"+strconv.FormatBool(envelope), func(t *testing.T) {
				// Arrange: both representations have admissible bytes/depth but too many nodes.
				contract, err := NewContract(WithRawPayloads())
				require.NoError(t, err)
				var encoded string
				switch channel {
				case InputMessages, OutputMessages:
					encoded = `[` + strings.Repeat(
						`{"role":"user","parts":[]},`,
						maxPayloadEntries/3,
					) + `{"role":"user","parts":[]}]`
				case SystemInstructions:
					encoded = `[` + strings.Repeat(`{"type":"text"},`, maxPayloadEntries/2) + `{"type":"text"}]`
				default:
					encoded = `[` + strings.Repeat(`0,`, maxPayloadEntries) + `0]`
				}
				if envelope {
					encoded = `{"truncated":true,"data":` + encoded + `}`
				}
				// Act.
				valid := validPayloadEvidence(contract, PayloadEvidence{Attribute: channel, Value: encoded})
				// Assert: no tool early return or truncation envelope bypasses structural limits.
				require.False(t, valid)
			})
		}
	}
}

func TestConformanceNegativeEvidenceDerivedFromRealExports(t *testing.T) {
	for _, scenario := range []string{"duplicate", "known_zero_missing", "wrong_unit", "wrong_enum", "missing_payload"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange: expected count/zero are fixed scenario facts; actuals come from SDK exports.
			tracker, provider, metricExporter, traceExporter := newTestTrackerWithMetrics(t, WithRawPayloads())
			meta := testMeta()
			meta.Duration = 0
			require.NoError(t, tracker.RecordInteraction(
				context.Background(),
				meta,
				Payload{
					InputMessages: []Message{
						{Role: "user", Parts: []ContentPart{{Type: "text", Content: "private-evidence-fixture"}}},
					},
				},
				Usage{OutputTokens: Observe(0, UnitTokens, SourceMeasured)},
			))
			flushTestProvider(t, provider)
			spans := traceExporter.GetSpans()
			require.Len(t, spans, 1)
			contract := tracker.Contract()
			profile := contract.Profile()
			observations := projectLifecycleWire(t, profile, spans)
			require.Len(t, observations, 1)
			metrics := metrytest.CollectResourceMetrics(t, provider, metricExporter)
			tokens := metrytest.FindInt64Histogram(
				t,
				metrics,
				metrytest.MetricSelector{Name: OutputTokenUsageMetricName},
			)
			require.Len(t, tokens.DataPoints, 1)
			observations[0].Accounting = &AccountingEvidence{
				ExpectedSpans: 1,
				ActualSpans:   len(spans),
				Expected: []AccountingMeasurement{
					{Instrument: OutputTokenUsageMetricName, Count: 1, IntegerTotal: 0},
				},
				Actual: []AccountingMeasurement{
					{
						Instrument:   OutputTokenUsageMetricName,
						Count:        tokens.DataPoints[0].Count,
						IntegerTotal: tokens.DataPoints[0].Sum,
					},
				},
			}
			declarations := []AdapterDeclaration{
				{
					ProfileID:  Schema().ID(),
					Mapping:    Schema().Mapping(),
					Boundaries: []string{profileModelBoundary},
					Units:      []FieldUnitDeclaration{{Field: OutputTokens, Unit: UnitTokens}},
				},
			}
			fixture := func() ([]ObservedBoundary, error) { return observations, nil }
			baseline, err := CheckConformance(contract, declarations, fixture)
			require.NoError(t, err)
			require.True(t, baseline.Passed(), "%+v", baseline.Checks)
			// Act: corrupt only actual evidence after proving the real export passes.
			mutateExportedConformanceEvidence(t, &observations[0], scenario)
			report, err := CheckConformance(contract, declarations, fixture)
			// Assert: each isolated defect rejects the previously valid exported fixture.
			require.NoError(t, err)
			require.False(t, report.Passed(), "%+v", report.Checks)
			require.NotContains(t, fmt.Sprint(report), "private-evidence-fixture")
		})
	}
}

func mutateExportedConformanceEvidence(t *testing.T, observed *ObservedBoundary, scenario string) {
	t.Helper()
	switch scenario {
	case "duplicate":
		observed.Accounting.Actual[0].Count++
	case "known_zero_missing":
		observed.Accounting.Actual = nil
	case "wrong_unit":
		found := false
		for index := range observed.Fields {
			if observed.Fields[index].Name == OutputTokens {
				observed.Fields[index].Unit = UnitFrames
				found = true
			}
		}
		require.True(t, found)
	case "wrong_enum":
		found := false
		for index := range observed.Enums {
			if observed.Enums[index].Name == OperationStatus {
				observed.Enums[index].Value = "private-evidence-fixture"
				found = true
			}
		}
		require.True(t, found)
	case "missing_payload":
		require.NotEmpty(t, observed.Payloads)
		observed.Payloads = nil
	}
}

func TestConformanceDuplicateToolAccountingFromRealExports(t *testing.T) {
	// Arrange: the fixture exports one model and one tool operation. It derives
	// measurements from the SDK, while scenario counts and host timing bounds are independent.
	profile := DefaultProfile()
	contract, err := NewContract(WithProfile(profile))
	require.NoError(t, err)
	observations := lifecycleExporterFixture(t, profile, "sync")
	declarations := []AdapterDeclaration{
		{ProfileID: Schema().ID(), Mapping: Schema().Mapping(), Boundaries: []string{profileModelBoundary}},
		{ProfileID: Schema().ID(), Mapping: Schema().Mapping(), Boundaries: []string{"tool"}},
	}
	fixture := func() ([]ObservedBoundary, error) { return observations, nil }
	baseline, err := CheckConformance(contract, declarations, fixture)
	require.NoError(t, err)
	require.True(t, baseline.Passed(), "%+v", baseline.Checks)
	// Act: duplicate only the exported tool measurement count.
	mutated := false
	for index := range observations {
		if observations[index].Boundary == "tool" {
			observations[index].Accounting.Actual[0].Count++
			mutated = true
		}
	}
	require.True(t, mutated)
	report, err := CheckConformance(contract, declarations, fixture)
	// Assert.
	require.NoError(t, err)
	require.False(t, report.Passed(), "%+v", report.Checks)
}
