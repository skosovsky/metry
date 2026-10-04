package genai

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

// PayloadEvidence identifies the controlled attribute carrying actual wire JSON.
type PayloadEvidence struct {
	Attribute string
	JSON      string
}

// AccountingMeasurement is an isolated instrument witness. Count is observation
// count, not series count. Counter fixtures must observe writes separately when
// the exporter provides only sums; absence of such evidence cannot prove a zero
// write or exclude duplicate writes. Totals use the instrument's declared unit.
// Integer instruments use IntegerTotal exactly; floating-point instruments use
// Total. The unused channel must be zero.
type AccountingMeasurement struct {
	Instrument   string
	Count        uint64
	Total        float64
	IntegerTotal int64
}

// AccountingEvidence compares host-owned fixture expectations with actual
// exported observations/write witnesses for one isolated telemetry boundary.
// Expectations are scenario facts, not copied from the adapter's recording input.
type AccountingEvidence struct {
	ExpectedSpans int
	ActualSpans   int
	Expected      []AccountingMeasurement
	Actual        []AccountingMeasurement
}

// ObservedEnum carries an actual wire value. Reports never repeat that value.
type ObservedEnum struct {
	Name  string
	Value string
}

// ObservedEvent carries the fixture's observed instrumentation owner and fields.
// Ownership identifies the emitter, not necessarily the parent span's tracer.
type ObservedEvent struct {
	Name   string
	Owner  string
	Fields []ProfileField
	Enums  []ObservedEnum
}

func validateFieldUnits(profile Profile, units []FieldUnitDeclaration) error {
	seen := make(map[string]bool)
	for _, unit := range units {
		field, found := declaredField(profile, unit.Field)
		if !found || field.Unit == "" || field.Unit != unit.Unit || seen[unit.Field] {
			return invalidObservation("adapter_unit", "incompatible")
		}
		seen[unit.Field] = true
	}
	return nil
}

func declaredField(profile Profile, name string) (ProfileField, bool) {
	for _, field := range profile.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return ProfileField{}, false
}

func (r *ConformanceReport) checkEvidence(
	index int, profile Profile, raw bool, declarations []AdapterDeclaration, observation ObservedBoundary,
) {
	if len(observation.Enums) > maxProfileEntries || len(observation.Events) > maxProfileEntries ||
		len(observation.Fields) > maxProfileEntries || len(observation.Payloads) > maxProfileEntries ||
		len(observation.Series) > maxProfileEntries {
		r.add(index, "", "evidence_size", "bounded", conformanceInvalid, false)
		return
	}
	unitValid := observedOwnerUnits(declarations, observation)
	r.add(index, "", "owner_units", "declared", categorical(unitValid), unitValid)
	boundary, known := findBoundary(profile, observation.Boundary)
	accounting := known && validAccounting(boundary, observation)
	r.add(index, "", "accounting", "single_boundary", categorical(accounting), accounting)
	enums := validObservedEnums(profile, raw, observation.Fields, observation.Enums)
	r.add(index, "", "enum_values", "declared_or_policy_encoded", categorical(enums), enums)
	payloads := completePayloadEvidence(observation)
	r.add(index, "", "payload_evidence", "complete_named_channels", categorical(payloads), payloads)
	for _, event := range observation.Events {
		valid := validObservedEvent(profile, raw, event)
		r.add(index, "", "event_schema", "declared_owner_and_fields", categorical(valid), valid)
	}
	for _, payload := range observation.Payloads {
		valid := validPayloadEvidence(profile, raw, payload)
		r.add(index, "", "payload_schema", "declared_channel_and_enums", categorical(valid), valid)
	}
}

func completePayloadEvidence(observation ObservedBoundary) bool {
	seen := make(map[string]bool, len(observation.Payloads))
	for _, payload := range observation.Payloads {
		if seen[payload.Attribute] {
			return false
		}
		seen[payload.Attribute] = true
	}
	for _, field := range observation.Fields {
		if field.Encoding == EncodingJSON && !seen[field.Name] {
			return false
		}
	}
	return true
}

func validPayloadEvidence(profile Profile, raw bool, payload PayloadEvidence) bool {
	field, found := declaredField(profile, payload.Attribute)
	if !found || field.Encoding != EncodingJSON || len(payload.JSON) > defaultMaxContextLength ||
		!utf8.ValidString(payload.JSON) || !json.Valid([]byte(payload.JSON)) || !boundedJSONDepth(payload.JSON) {
		return false
	}
	if payload.Attribute == ToolCallArguments || payload.Attribute == ToolCallResult {
		return true // Host-owned keys must never be interpreted as library metadata.
	}
	data := json.RawMessage(payload.JSON)
	var envelope struct {
		Data      json.RawMessage `json:"data"`
		Truncated bool            `json:"truncated"`
	}
	if len(data) > 0 && data[0] == '{' && json.Unmarshal(data, &envelope) == nil && envelope.Truncated {
		if !decodePayloadShape(data, &envelope) {
			return false
		}
		if len(envelope.Data) == 0 {
			return true
		}
		data = envelope.Data
	}
	switch payload.Attribute {
	case InputMessages, OutputMessages:
		var messages []Message
		if !decodePayloadShape(data, &messages) {
			return false
		}
		for _, message := range messages {
			if !optionalPayloadEnum(profile, raw, "message.role", message.Role) ||
				!optionalPayloadEnum(profile, raw, "message.finish_reason", message.FinishReason) ||
				!validPartEnums(profile, raw, message.Parts) {
				return false
			}
		}
		return true
	case SystemInstructions:
		var parts []ContentPart
		return decodePayloadShape(data, &parts) && validPartEnums(profile, raw, parts)
	default:
		return false
	}
}

func decodePayloadShape(data []byte, result any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(result) != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func optionalPayloadEnum(profile Profile, raw bool, name, value string) bool {
	return value == "" || enumValueAllowed(profile, raw, ObservedEnum{Name: name, Value: value})
}

func validPartEnums(profile Profile, raw bool, parts []ContentPart) bool {
	for _, part := range parts {
		if !optionalPayloadEnum(profile, raw, "content.type", part.Type) {
			return false
		}
	}
	return true
}

func observedOwnerUnits(declarations []AdapterDeclaration, observation ObservedBoundary) bool {
	for _, declaration := range declarations {
		if !slices.Contains(declaration.Boundaries, observation.Boundary) {
			continue
		}
		if len(declaration.Units) == 0 {
			return true
		}
		for _, field := range observation.Fields {
			if field.Unit != "" &&
				!slices.Contains(declaration.Units, FieldUnitDeclaration{Field: field.Name, Unit: field.Unit}) {
				return false
			}
		}
		for _, field := range observedUsageUnits(observation.Usage) {
			if !slices.Contains(declaration.Units, field) {
				return false
			}
		}
		return true
	}
	return false
}

func observedUsageUnits(usage Usage) []FieldUnitDeclaration {
	units := make([]FieldUnitDeclaration, 0, attrCapUsageAttrs)
	for _, value := range []struct {
		name    string
		present bool
		unit    Unit
	}{
		{InputTokens, usage.InputTokens.Present, usage.InputTokens.Unit},
		{OutputTokens, usage.OutputTokens.Present, usage.OutputTokens.Unit},
		{CacheReadInputTokens, usage.CacheReadInputTokens.Present, usage.CacheReadInputTokens.Unit},
		{CacheCreationInputTokens, usage.CacheCreationInputTokens.Present, usage.CacheCreationInputTokens.Unit},
		{UsageReasoningOutputTokens, usage.ReasoningOutputTokens.Present, usage.ReasoningOutputTokens.Unit},
		{UsageCost, usage.Cost.Present, usage.Cost.Unit},
		{AudioSeconds, usage.AudioSeconds.Present, usage.AudioSeconds.Unit},
		{ImageCount, usage.ImageCount.Present, usage.ImageCount.Unit},
		{UsageVideoSeconds, usage.VideoSeconds.Present, usage.VideoSeconds.Unit},
		{UsageVideoFrames, usage.VideoFrames.Present, usage.VideoFrames.Unit},
	} {
		if value.present {
			units = append(units, FieldUnitDeclaration{Field: value.name, Unit: value.unit})
		}
	}
	return units
}

func validAccounting(boundary Boundary, observation ObservedBoundary) bool {
	evidence := observation.Accounting
	if evidence == nil || evidence.ExpectedSpans < 0 || evidence.ExpectedSpans > maxProfileEntries ||
		evidence.ActualSpans != evidence.ExpectedSpans || len(evidence.Expected) > maxProfileEntries ||
		len(evidence.Actual) > maxProfileEntries {
		return false
	}
	expected, expectedOK := accountingMap(evidence.Expected)
	actual, actualOK := accountingMap(evidence.Actual)
	if !expectedOK || !actualOK || len(expected) != len(actual) {
		return false
	}
	for instrument, wanted := range expected {
		got, found := actual[instrument]
		if !found || got.Count != wanted.Count || got.Total != wanted.Total || got.IntegerTotal != wanted.IntegerTotal {
			return false
		}
		if !boundary.Accounting && (got.Count != 0 || got.Total != 0 || got.IntegerTotal != 0) {
			return false
		}
	}
	if !boundary.Accounting {
		for _, series := range observation.Series {
			if series.Count != 0 {
				return false
			}
		}
		return true
	}
	return usageAccountingMatches(observation.Usage, expected)
}

func accountingMap(items []AccountingMeasurement) (map[string]AccountingMeasurement, bool) {
	result := make(map[string]AccountingMeasurement, len(items))
	for _, item := range items {
		if safeInstrument(item.Instrument) == "" || math.IsNaN(item.Total) || math.IsInf(item.Total, 0) ||
			item.Total < 0 || item.IntegerTotal < 0 ||
			(item.Count == 0 && (item.Total != 0 || item.IntegerTotal != 0)) {
			return nil, false
		}
		integerInstrument := slices.Contains(
			[]string{TokenUsageMetricName, TokenComponentUsageMetricName, VideoFramesMetricName},
			item.Instrument,
		)
		if (integerInstrument && item.Total != 0) || (!integerInstrument && item.IntegerTotal != 0) {
			return nil, false
		}
		if _, duplicate := result[item.Instrument]; duplicate {
			return nil, false
		}
		result[item.Instrument] = item
	}
	return result, true
}

func usageAccountingMatches(usage Usage, expected map[string]AccountingMeasurement) bool {
	required := make(map[string]AccountingMeasurement)
	for _, item := range []struct {
		instrument string
		present    bool
		value      float64
		integer    int64
	}{
		{TokenUsageMetricName, usage.InputTokens.Present, 0, int64(usage.InputTokens.Value)},
		{TokenUsageMetricName, usage.OutputTokens.Present, 0, int64(usage.OutputTokens.Value)},
		{TokenComponentUsageMetricName, usage.CacheReadInputTokens.Present, 0, int64(usage.CacheReadInputTokens.Value)},
		{TokenComponentUsageMetricName, usage.CacheCreationInputTokens.Present, 0, int64(usage.CacheCreationInputTokens.Value)},
		{TokenComponentUsageMetricName, usage.ReasoningOutputTokens.Present, 0, int64(usage.ReasoningOutputTokens.Value)},
		{CostMetricName, usage.Cost.Present, usage.Cost.Value, 0},
		{VideoSecondsMetricName, usage.VideoSeconds.Present, usage.VideoSeconds.Value, 0},
		{VideoFramesMetricName, usage.VideoFrames.Present, 0, int64(usage.VideoFrames.Value)},
	} {
		if item.present {
			value := required[item.instrument]
			value.Instrument = item.instrument
			value.Count++
			if item.integer > math.MaxInt64-value.IntegerTotal {
				return false
			}
			value.IntegerTotal += item.integer
			value.Total += item.value
			required[item.instrument] = value
		}
	}
	for _, instrument := range []string{TokenUsageMetricName, TokenComponentUsageMetricName, CostMetricName,
		VideoSecondsMetricName, VideoFramesMetricName} {
		wanted := required[instrument]
		got := expected[instrument]
		if wanted.Count != got.Count || wanted.Total != got.Total || wanted.IntegerTotal != got.IntegerTotal {
			return false
		}
	}
	return true
}

func validObservedEnums(profile Profile, raw bool, fields []ProfileField, values []ObservedEnum) bool {
	seen := make(map[string]bool)
	for _, value := range values {
		if seen[value.Name] || !enumValueAllowed(profile, raw, value) {
			return false
		}
		seen[value.Name] = true
	}
	for _, field := range fields {
		for _, enum := range profile.Enumerations() {
			if enum.Name == field.Name && !seen[field.Name] {
				return false
			}
		}
	}
	return true
}

func enumValueAllowed(profile Profile, raw bool, value ObservedEnum) bool {
	if len(value.Value) > maxPayloadInputBytes {
		return false
	}
	for _, enum := range profile.Enumerations() {
		if enum.Name != value.Name {
			continue
		}
		if slices.Contains(enum.Values, value.Value) {
			return true
		}
		if !enum.PolicyEncodedUnknown {
			return false
		}
		if raw {
			return true
		}
		switch profile.PayloadPolicyID {
		case profileRedactPolicy:
			return value.Value == redactedPayloadText
		case profileHashPolicy:
			return validFingerprint(value.Value)
		default:
			return false
		}
	}
	return false
}

func validFingerprint(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func validObservedEvent(profile Profile, raw bool, observed ObservedEvent) bool {
	if len(observed.Fields) > maxProfileEntries || len(observed.Enums) > maxProfileEntries {
		return false
	}
	for _, event := range profile.Events() {
		if event.Name != observed.Name {
			continue
		}
		if event.Owner != observed.Owner {
			return false
		}
		seen := make(map[string]bool)
		for _, field := range observed.Fields {
			declared, found := declaredField(profile, field.Name)
			if !found || field != declared || !slices.Contains(event.Fields, field.Name) || seen[field.Name] {
				return false
			}
			seen[field.Name] = true
		}
		return validObservedEnums(profile, raw, observed.Fields, observed.Enums)
	}
	return false
}
