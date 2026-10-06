package genai

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxPayloadInputBytes = 1 << 20
	maxPayloadEntries    = 4096
	truncatedJSONMarker  = `{"truncated":true}`
)

// boundedJSON never slices serialized JSON. On overflow it retains a prefix of
// top-level elements/fields in a structured envelope, or only the marker. The
// binary search bounds serialization passes independently of element count.
func boundedJSON(value any, limit int) (string, error) {
	if limit < len(truncatedJSONMarker) {
		return "", invalidObservation("payload_budget", "too_small")
	}
	buf, err := json.Marshal(value)
	if err != nil {
		return "", invalidObservation("payload_json", "invalid_encoding")
	}
	if string(buf) == "null" || string(buf) == "[]" {
		return "", nil
	}
	if len(buf) > maxPayloadInputBytes || !boundedJSONDepth(string(buf)) {
		return "", invalidObservation("payload_json", "input_bounds")
	}
	decoded, err := decodePayloadJSON(string(buf))
	if err != nil {
		return "", err
	}
	if len(buf) <= limit {
		return string(buf), nil
	}
	count, prefix := jsonPrefix(decoded)
	best := truncatedJSONMarker
	low, high := 0, count
	for low <= high {
		middle := low + (high-low)/2
		envelope := map[string]any{"data": prefix(middle), "truncated": true}
		remaining := maxPayloadEntries
		if !jsonNodesWithinBudget(envelope, &remaining) {
			high = middle - 1
			continue
		}
		candidate, marshalErr := json.Marshal(envelope)
		if marshalErr != nil {
			return "", invalidObservation("payload_json", "invalid_encoding")
		}
		if len(candidate) <= limit && boundedJSONDepth(string(candidate)) {
			best = string(candidate)
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return best, nil
}

func jsonPrefix(value any) (int, func(int) any) {
	switch typed := value.(type) {
	case []any:
		return len(typed), func(count int) any { return typed[:count] }
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		return len(keys), func(count int) any {
			out := make(map[string]any, count)
			for _, key := range keys[:count] {
				out[key] = typed[key]
			}
			return out
		}
	default:
		return 0, func(int) any { return nil }
	}
}

// Validate limits work before any sanitizer allocates copies or hashes content.
func validatePayloadBounds(payload Payload) error {
	return validatePayloadEncodingBounds(payload, true)
}

func validatePayloadEncodingBounds(payload Payload, input bool) error {
	if len(payload.SystemInstructions) > maxPayloadEntries || len(payload.InputMessages) > maxPayloadEntries ||
		len(payload.OutputMessages) > maxPayloadEntries {
		return invalidObservation("payload", "input_bounds")
	}
	budget := payloadInputBudget{bytes: 0, parts: 0}
	if !budget.addParts(payload.SystemInstructions, input) {
		return invalidObservation("payload", "input_bounds")
	}
	for _, messages := range [][]Message{payload.InputMessages, payload.OutputMessages} {
		for _, message := range messages {
			if !utf8.ValidString(message.Role) {
				return invalidObservation("payload_json", "invalid_encoding")
			}
			budget.bytes += len(message.Role)
			if budget.bytes > maxPayloadInputBytes || !budget.addParts(message.Parts, input) {
				return invalidObservation("payload", "input_bounds")
			}
		}
	}
	return validatePayloadChannels(payload)
}

// Validate the actual controlled JSON shape, including DTO wrappers and escaping.
// The encoded bytes are only inspected; original RawMessage bytes remain intact
// for custom policy callbacks.
func validatePayloadChannels(payload Payload) error {
	for _, channel := range []any{payload.SystemInstructions, payload.InputMessages, payload.OutputMessages} {
		encoded, err := json.Marshal(channel)
		if err != nil {
			return invalidObservation("payload_json", "invalid_encoding")
		}
		if _, err := decodePayloadJSON(string(encoded)); err != nil {
			return err
		}
	}
	return nil
}

type payloadInputBudget struct{ bytes, parts int }

func (b *payloadInputBudget) addParts(items []ContentPart, input bool) bool {
	b.parts += len(items)
	if b.parts > maxPayloadEntries {
		return false
	}
	for _, part := range items {
		if !validContentPartStrings(part) {
			return false
		}
		if input && mediaBodyIsInline(part) {
			return false
		}
		b.bytes += len(part.Type) + len(part.Content) + len(part.ID) + len(part.Name) +
			len(part.Arguments) + len(part.Response)
		if b.bytes > maxPayloadInputBytes {
			return false
		}
		for _, raw := range []json.RawMessage{part.Arguments, part.Response} {
			if len(raw) != 0 {
				if _, err := decodePayloadJSON(string(raw)); err != nil {
					return false
				}
			}
		}
	}
	return true
}

func mediaBodyIsInline(part ContentPart) bool {
	if !slices.Contains(
		[]string{payloadImageType, payloadImageURLType, payloadAudioType, payloadVideoType, payloadFileType},
		part.Type,
	) ||
		part.Content == "" {
		return false
	}
	// Only opaque references are observed. No data URI/base64/inline binary body
	// is accepted, even in raw mode; metry never fetches these references.
	return !strings.HasPrefix(part.Content, "https://") && !strings.HasPrefix(part.Content, "http://") &&
		!strings.HasPrefix(part.Content, "urn:")
}

func validateToolJSONInput(raw string) error {
	if len(raw) > maxPayloadInputBytes || !boundedJSONDepth(raw) {
		return invalidObservation("tool_payload", "input_bounds")
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if json.Valid([]byte(raw)) {
		_, err := decodePayloadJSON(raw)
		return err
	}
	if strings.ContainsAny(trimmed[:1], "{[\"") {
		return invalidObservation("tool_payload", "invalid_encoding")
	}
	return nil
}

func boundedToolJSON(raw string, cfg recorderConfig) (string, error) {
	if raw == "" {
		return "", nil
	}
	if err := validateToolJSONInput(raw); err != nil {
		return "", err
	}
	if json.Valid([]byte(raw)) {
		return boundedJSON(json.RawMessage(raw), cfg.PayloadByteLimit())
	}
	// Tool inputs may be plain text. Encode them as a JSON string rather than
	// emitting malformed structured content or silently interpreting fragments.
	return boundedJSON(raw, cfg.PayloadByteLimit())
}

func recordPayloadDiagnostic(span trace.Span, err error) {
	if diagnostic, ok := errors.AsType[*ObservationError](err); ok {
		span.AddEvent("metry.observation.rejected", trace.WithAttributes(
			attribute.String("metry.diagnostic.field", diagnostic.Field),
			attribute.String("metry.diagnostic.rule", diagnostic.Rule),
		))
	}
}

// boundedJSONArray retains only complete top-level elements. Output always has
// the upstream array shape; truncation is carried by an independent attribute.
func boundedJSONArray(value any, limit int) (string, bool, error) {
	if limit < len(truncatedJSONMarker) {
		return "", false, invalidObservation("payload_budget", "too_small")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", false, invalidObservation("payload_json", "invalid_encoding")
	}
	decoded, err := decodePayloadJSON(string(encoded))
	if err != nil {
		return "", false, err
	}
	if decoded == nil {
		return "", false, nil
	}
	items, ok := decoded.([]any)
	if !ok {
		return "", false, invalidObservation("payload_json", "array_required")
	}
	if len(items) == 0 {
		return "", false, nil
	}
	if len(encoded) <= limit {
		return string(encoded), false, nil
	}
	best := "[]"
	retained := 0
	low, high := 0, len(items)
	for low <= high {
		middle := low + (high-low)/2
		candidate, marshalErr := json.Marshal(items[:middle])
		if marshalErr != nil {
			return "", false, invalidObservation("payload_json", "invalid_encoding")
		}
		if len(candidate) <= limit {
			best = string(candidate)
			retained = middle
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return best, retained < len(items), nil
}

func payloadTruncationKey(channel string) string {
	switch channel {
	case SystemInstructions:
		return "metry.gen_ai.payload.system_instructions.truncated"
	case InputMessages:
		return "metry.gen_ai.payload.input_messages.truncated"
	case OutputMessages:
		return "metry.gen_ai.payload.output_messages.truncated"
	default:
		return ""
	}
}

func validContentPartStrings(part ContentPart) bool {
	for _, value := range []string{part.Type, part.Content, part.ID, part.Name} {
		if !utf8.ValidString(value) {
			return false
		}
	}
	return true
}
