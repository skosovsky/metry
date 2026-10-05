package genai

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConformanceAccountingAndWireEvidenceRejectSafeMismatches(t *testing.T) {
	for _, scenario := range []string{"logical", "duplicate", "missing_zero", "missing_evidence", "owner_units",
		"enum", "missing_enum", "event_name", "event_owner", "event_field", "payload_enum",
		"missing_payload", "duplicate_payload", "envelope_fields", "duplicate_tool"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange: expected known-zero usage and actual exporter/write witnesses.
			profile := DefaultProfile()
			declaration := AdapterDeclaration{ProfileID: profile.ID, Mapping: profile.Mapping,
				Boundaries: []string{profileModelBoundary}}
			observation := zeroAccountingObservation(profile)
			switch scenario {
			case "logical":
				observation.Boundary, observation.Operation = string(IdentityLogical), string(IdentityLogical)
				declaration.Boundaries = []string{string(IdentityLogical)}
			case "duplicate":
				observation.Accounting.Actual[0].Count = 2
			case "duplicate_tool":
				declaration.Boundaries = []string{"tool"}
				observation = ObservedBoundary{Boundary: "tool", Operation: profileToolOperation,
					Accounting: &AccountingEvidence{ExpectedSpans: 1, ActualSpans: 1,
						Expected: []AccountingMeasurement{{Instrument: ToolDurationMetricName, Count: 1, Total: 1}},
						Actual:   []AccountingMeasurement{{Instrument: ToolDurationMetricName, Count: 2, Total: 1}}}}
				field, _ := declaredField(profile, OperationName)
				observation.Fields = []ProfileField{field}
			case "missing_zero":
				observation.Accounting.Actual = nil
			case "missing_evidence":
				observation.Accounting = nil
			case "owner_units":
				declaration.Units = []FieldUnitDeclaration{{Field: UsageCost, Unit: UnitCurrency}}
			case "enum":
				observation.Enums[0].Value = "private_fixture_secret"
			case "missing_enum":
				observation.Enums = nil
			case "event_name", "event_owner", "event_field":
				event := ObservedEvent{Name: "gen_ai.evaluation.result", Owner: "metry.genai"}
				switch scenario {
				case "event_name":
					event.Name = "private_fixture_secret"
				case "event_owner":
					event.Owner = "private_fixture_secret"
				default:
					event.Fields = []ProfileField{{Name: "private_fixture_secret"}}
				}
				observation.Events = []ObservedEvent{event}
			case "payload_enum":
				observation.Payloads = []PayloadEvidence{{Attribute: InputMessages,
					Value: `[{"role":"private_fixture_secret","parts":[]}]`}}
			case "missing_payload":
				field, _ := declaredField(profile, InputMessages)
				observation.Fields = append(observation.Fields, field)
			case "duplicate_payload":
				observation.Payloads = []PayloadEvidence{
					{Attribute: InputMessages, Value: `[]`}, {Attribute: InputMessages, Value: `[]`},
				}
			case "envelope_fields":
				observation.Payloads = []PayloadEvidence{{Attribute: InputMessages,
					Value: `{"truncated":true,"private_fixture_secret":"secret"}`}}
			}
			// Act.
			report, err := CheckConformance(testContract(profile, false), []AdapterDeclaration{declaration},
				func() ([]ObservedBoundary, error) { return []ObservedBoundary{observation}, nil })
			// Assert: secret names/values never enter diagnostics or report fields.
			require.NoError(t, err)
			assert.False(t, report.Passed(), "%+v", report.Checks)
			assert.NotContains(t, fmt.Sprint(report), "private_fixture_secret")
		})
	}
}

func TestConformanceToolPayloadKeepsHostOwnedEnvelopeLikeKeys(t *testing.T) {
	for _, channel := range []string{ToolCallArguments, ToolCallResult} {
		// Arrange: these are business JSON keys, not a typed message envelope.
		profile := DefaultProfile()
		declaration := AdapterDeclaration{ProfileID: profile.ID, Mapping: profile.Mapping,
			Boundaries: []string{"tool"}}
		observation := ObservedBoundary{Boundary: "tool", Operation: profileToolOperation,
			Accounting: &AccountingEvidence{ExpectedSpans: 1, ActualSpans: 1},
			Payloads: []PayloadEvidence{{Attribute: channel,
				Value: `{"truncated":true,"reason":"private_fixture_secret"}`}}}
		operationField, _ := declaredField(profile, OperationName)
		payloadField, _ := declaredField(profile, channel)
		observation.Fields = []ProfileField{operationField, payloadField}
		// Act.
		report, err := CheckConformance(testContract(profile, false), []AdapterDeclaration{declaration},
			func() ([]ObservedBoundary, error) { return []ObservedBoundary{observation}, nil })
		// Assert: BYOT structure is accepted without echoing its content.
		require.NoError(t, err)
		assert.True(t, report.Passed(), "%+v", report.Checks)
		assert.NotContains(t, fmt.Sprint(report), "private_fixture_secret")
	}
}

func TestConformanceIntegerAccountingPreservesPrecision(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		// Arrange: adjacent integers beyond float64's exact integer range.
		profile := DefaultProfile()
		declaration := AdapterDeclaration{ProfileID: profile.ID, Mapping: profile.Mapping,
			Boundaries: []string{profileModelBoundary}}
		observation := zeroAccountingObservation(profile)
		const tokens = int64(1<<53 + 1)
		observation.Usage.InputTokens = Observe(int(tokens), UnitTokens, SourceMeasured)
		observation.Accounting.Expected[0].IntegerTotal = tokens
		observation.Accounting.Actual[0].IntegerTotal = tokens
		if mismatch {
			observation.Accounting.Actual[0].IntegerTotal--
		}
		// Act.
		report, err := CheckConformance(testContract(profile, false), []AdapterDeclaration{declaration},
			func() ([]ObservedBoundary, error) { return []ObservedBoundary{observation}, nil })
		// Assert: float conversion must not hide a one-token discrepancy.
		require.NoError(t, err)
		assert.Equal(t, !mismatch, report.Passed(), "%+v", report.Checks)
	}
}

func zeroAccountingObservation(profile Profile) ObservedBoundary {
	fields := make([]ProfileField, 0, 4)
	for _, name := range []string{OperationName, InputTokens, measurementProvenanceKey(InputTokens) + ".source", measurementProvenanceKey(InputTokens) + ".unit"} {
		field, _ := declaredField(profile, name)
		fields = append(fields, field)
	}
	return ObservedBoundary{
		Boundary: profileModelBoundary, Operation: profileChatOperation, Fields: fields,
		Usage: Usage{InputTokens: Observe(0, UnitTokens, SourceMeasured)},
		Accounting: &AccountingEvidence{ExpectedSpans: 1, ActualSpans: 1,
			Expected: []AccountingMeasurement{{Instrument: InputTokenUsageMetricName, Count: 1, Total: 0}},
			Actual:   []AccountingMeasurement{{Instrument: InputTokenUsageMetricName, Count: 1, Total: 0}}},
		Enums: []ObservedEnum{{Name: measurementProvenanceKey(InputTokens) + ".source", Value: string(SourceMeasured)},
			{Name: measurementProvenanceKey(InputTokens) + ".unit", Value: string(UnitTokens)}},
	}
}

func TestConformanceRejectsDuplicateModelTimingMeasurements(t *testing.T) {
	for _, instrument := range []string{OperationDurationMetricName, TTFTMetricName,
		DerivedOutputTokensPerSecondMetricName, DerivedMeanOutputTokenIntervalMetricName} {
		t.Run(instrument, func(t *testing.T) {
			// Arrange: one scenario observation, two actual histogram writes.
			profile := DefaultProfile()
			declaration := AdapterDeclaration{ProfileID: profile.ID, Mapping: profile.Mapping,
				Boundaries: []string{profileModelBoundary}}
			observation := zeroAccountingObservation(profile)
			observation.Accounting.Expected = append(observation.Accounting.Expected,
				AccountingMeasurement{Instrument: instrument, Count: 1, Total: 1})
			observation.Accounting.Actual = append(observation.Accounting.Actual,
				AccountingMeasurement{Instrument: instrument, Count: 2, Total: 1})
			// Act.
			report, err := CheckConformance(testContract(profile, false), []AdapterDeclaration{declaration},
				func() ([]ObservedBoundary, error) { return []ObservedBoundary{observation}, nil })
			// Assert: timing/quality counts are not mistaken for series counts.
			require.NoError(t, err)
			assert.False(t, report.Passed(), "%+v", report.Checks)
		})
	}
}

func TestConformanceKnownZeroAndPolicyEncodedPayloadEnums(t *testing.T) {
	for _, policy := range []string{profileRedactPolicy, profileHashPolicy, profileRawPolicy} {
		// Arrange.
		profile := DefaultProfile()
		profile.PayloadPolicyID = policy
		profile.AllowRaw = policy == profileRawPolicy
		declaration := AdapterDeclaration{ProfileID: profile.ID, Mapping: profile.Mapping,
			Boundaries: []string{
				profileModelBoundary,
			}, Units: []FieldUnitDeclaration{{Field: InputTokens, Unit: UnitTokens}}}
		observation := zeroAccountingObservation(profile)
		role := redactedPayloadText
		switch policy {
		case profileHashPolicy:
			role = fingerprintPayloadLabel("private_fixture_secret")
		case profileRawPolicy:
			role = "private_fixture_secret"
		}
		observation.Payloads = []PayloadEvidence{{Attribute: InputMessages,
			Value: fmt.Sprintf(`[{"role":%q,"parts":[{"type":"tool_call","name":"lookup"}]}]`, role)}}
		field, _ := declaredField(profile, InputMessages)
		observation.Fields = append(observation.Fields, field)
		// Act.
		report, err := CheckConformance(testContract(profile, profile.AllowRaw), []AdapterDeclaration{declaration},
			func() ([]ObservedBoundary, error) { return []ObservedBoundary{observation}, nil })
		// Assert.
		require.NoError(t, err)
		assert.True(t, report.Passed(), "%+v", report.Checks)
		assert.NotContains(t, fmt.Sprint(report), "private_fixture_secret")
	}
}
