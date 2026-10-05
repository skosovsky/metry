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

// PayloadEvidence identifies the controlled attribute carrying actual exported wire value.
type PayloadEvidence struct {
	Attribute string
	Value     string
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
	Name     string
	Owner    string
	Fields   []ProfileField
	Enums    []ObservedEnum
	Payloads []PayloadEvidence
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
	index int, contract Contract, declarations []AdapterDeclaration, observation ObservedBoundary,
) {
	if len(observation.Enums) > maxProfileEntries || len(observation.Events) > maxProfileEntries ||
		len(observation.Fields) > maxProfileEntries || len(observation.Payloads) > maxProfileEntries ||
		len(observation.Series) > maxProfileEntries {
		r.add(index, "", "evidence_size", "bounded", conformanceInvalid, false)
		return
	}
	profile, raw := contract.Profile(), contract.RawEnabled()
	unitValid := observedOwnerUnits(declarations, observation)
	r.add(index, "", "owner_units", "declared", categorical(unitValid), unitValid)
	boundary, known := findBoundary(profile, observation.Boundary)
	accounting := known && validAccounting(boundary, observation)
	r.add(index, "", "accounting", "single_boundary", categorical(accounting), accounting)
	enums := validObservedEnums(profile, raw, observation.Fields, observation.Enums)
	r.add(index, "", "enum_values", "declared_or_policy_encoded", categorical(enums), enums)
	payloads := completePayloadEvidence(contract, observation.Fields, observation.Payloads)
	r.add(index, "", "payload_evidence", "complete_named_channels", categorical(payloads), payloads)
	for _, event := range observation.Events {
		valid := validObservedEvent(contract, event)
		r.add(index, "", "event_schema", "declared_owner_and_fields", categorical(valid), valid)
	}
	for _, payload := range observation.Payloads {
		valid := validPayloadEvidence(contract, payload)
		r.add(index, "", "payload_schema", "declared_channel_and_enums", categorical(valid), valid)
	}
}

func completePayloadEvidence(contract Contract, fields []ProfileField, payloads []PayloadEvidence) bool {
	seen := make(map[string]bool, len(payloads))
	for _, payload := range payloads {
		if seen[payload.Attribute] {
			return false
		}
		present := false
		for _, field := range fields {
			if field.Name == payload.Attribute {
				present = true
				break
			}
		}
		if !present {
			return false
		}
		seen[payload.Attribute] = true
	}
	for _, field := range fields {
		if !validTruncationField(field, seen) {
			return false
		}
		if _, controlled := contract.payloadChannel(field.Name); controlled && !seen[field.Name] {
			return false
		}
	}
	return true
}

func validPayloadEvidence(contract Contract, payload PayloadEvidence) bool {
	profile, raw := contract.Profile(), contract.RawEnabled()
	channel, found := contract.payloadChannel(payload.Attribute)
	if !found || len(payload.Value) > channel.OutputByteLimit || !utf8.ValidString(payload.Value) {
		return false
	}
	if channel.Encoding == EncodingString {
		return true
	}
	if _, err := decodePayloadJSON(payload.Value); err != nil {
		return false
	}
	if payload.Attribute == ToolCallArguments || payload.Attribute == ToolCallResult {
		return true // Host-owned keys must never be interpreted as library metadata.
	}
	data := json.RawMessage(payload.Value)
	if len(data) == 0 || data[0] != '[' {
		return false
	}
	switch payload.Attribute {
	case InputMessages, OutputMessages:
		return validMessageEvidence(data, profile, raw)
	case RetrievalDocuments:
		return validRetrievalDocumentsEvidence(data)
	case SystemInstructions:
		if !validContentWire(data, false) {
			return false
		}
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
		{CacheWriteInputTokens, usage.CacheWriteInputTokens.Present, usage.CacheWriteInputTokens.Unit},
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
			[]string{
				OutputTokenUsageMetricName,
				InputTokenUsageMetricName,
				TokenComponentUsageMetricName,
				VideoFramesMetricName,
			},
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
		{InputTokenUsageMetricName, usage.InputTokens.Present, 0, int64(usage.InputTokens.Value)},
		{OutputTokenUsageMetricName, usage.OutputTokens.Present, 0, int64(usage.OutputTokens.Value)},
		{TokenComponentUsageMetricName, usage.CacheReadInputTokens.Present, 0, int64(usage.CacheReadInputTokens.Value)},
		{TokenComponentUsageMetricName, usage.CacheWriteInputTokens.Present, 0, int64(usage.CacheWriteInputTokens.Value)},
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
	for _, instrument := range []string{OutputTokenUsageMetricName, InputTokenUsageMetricName, TokenComponentUsageMetricName, CostMetricName,
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

func validObservedEvent(contract Contract, observed ObservedEvent) bool {
	profile, raw := contract.Profile(), contract.RawEnabled()
	if len(observed.Fields) > maxProfileEntries || len(observed.Enums) > maxProfileEntries ||
		len(observed.Payloads) > maxProfileEntries {
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
		if !completePayloadEvidence(contract, observed.Fields, observed.Payloads) {
			return false
		}
		for _, payload := range observed.Payloads {
			if !validPayloadEvidence(contract, payload) {
				return false
			}
		}
		return validObservedEnums(profile, raw, observed.Fields, observed.Enums)
	}
	return false
}

// Reference values are opaque; validation never interprets or emits their contents.
func validRetrievalDocumentsEvidence(data []byte) bool {
	var documents []map[string]json.RawMessage
	if !decodePayloadShape(data, &documents) {
		return false
	}
	for _, document := range documents {
		if document == nil {
			return false
		}
		for key, raw := range document {
			switch key {
			case "id", "metry.document.version", "metry.document.digest":
				var value string
				if json.Unmarshal(raw, &value) != nil {
					return false
				}
			case "score":
				var value float64
				if json.Unmarshal(raw, &value) != nil || math.IsNaN(value) || math.IsInf(value, 0) {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

func validContentWire(data []byte, messages bool) bool {
	var parts []json.RawMessage
	if messages {
		var items []struct {
			Role  *string         `json:"role"`
			Parts json.RawMessage `json:"parts"`
		}
		if json.Unmarshal(data, &items) != nil {
			return false
		}
		for _, item := range items {
			if item.Role == nil || len(item.Parts) == 0 || item.Parts[0] != '[' ||
				!validContentWire(item.Parts, false) {
				return false
			}
		}
		return true
	}
	if json.Unmarshal(data, &parts) != nil {
		return false
	}
	for _, raw := range parts {
		if !validContentPartWire(raw) {
			return false
		}
	}
	return true
}

func validContentPartWire(raw json.RawMessage) bool {
	var item map[string]json.RawMessage
	if json.Unmarshal(raw, &item) != nil || item == nil {
		return false
	}
	var kind string
	if value, exists := item["type"]; !exists || json.Unmarshal(value, &kind) != nil {
		return false
	}
	key := ""
	switch kind {
	case payloadTextType:
		key = "content"
	case payloadToolCallType:
		key = "name"
	case payloadToolCallResponseType:
		_, exists := item["response"]
		return exists
	}
	if key == "" {
		return true
	}
	var value string
	field, exists := item[key]
	return exists && json.Unmarshal(field, &value) == nil
}

func validMessageEvidence(data []byte, profile Profile, raw bool) bool {
	if !validContentWire(data, true) {
		return false
	}
	var messages []Message
	if !decodePayloadShape(data, &messages) {
		return false
	}
	for _, message := range messages {
		if !optionalPayloadEnum(profile, raw, "message.role", message.Role) ||
			!validPartEnums(profile, raw, message.Parts) {
			return false
		}
	}
	return true
}

func validTruncationField(field ProfileField, channels map[string]bool) bool {
	for _, channel := range []string{SystemInstructions, InputMessages, OutputMessages, RetrievalDocuments} {
		marker := payloadTruncationKey(channel)
		if channel == RetrievalDocuments {
			marker = RetrievalDocumentsTruncated
		}
		if field.Name == marker {
			return channels[channel] && field.Encoding == EncodingBoolean
		}
	}
	return true
}
