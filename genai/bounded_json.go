package genai

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

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
	if len(buf) <= limit {
		return string(buf), nil
	}
	var decoded any
	if err := json.Unmarshal(buf, &decoded); err != nil {
		return "", invalidObservation("payload_json", "invalid_encoding")
	}
	count, prefix := jsonPrefix(decoded)
	best := truncatedJSONMarker
	low, high := 0, count
	for low <= high {
		middle := low + (high-low)/2
		envelope := struct {
			Data      any  `json:"data"`
			Truncated bool `json:"truncated"`
		}{Data: prefix(middle), Truncated: true}
		candidate, marshalErr := json.Marshal(envelope)
		if marshalErr != nil {
			return "", invalidObservation("payload_json", "invalid_encoding")
		}
		if len(candidate) <= limit {
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
	if len(payload.SystemInstructions) > maxPayloadEntries || len(payload.InputMessages) > maxPayloadEntries ||
		len(payload.OutputMessages) > maxPayloadEntries {
		return invalidObservation("payload", "input_bounds")
	}
	budget := payloadInputBudget{bytes: 0, parts: 0}
	if !budget.addParts(payload.SystemInstructions) {
		return invalidObservation("payload", "input_bounds")
	}
	for _, messages := range [][]Message{payload.InputMessages, payload.OutputMessages} {
		for _, message := range messages {
			budget.bytes += len(message.Role) + len(message.FinishReason)
			if budget.bytes > maxPayloadInputBytes || !budget.addParts(message.Parts) {
				return invalidObservation("payload", "input_bounds")
			}
		}
	}
	return nil
}

type payloadInputBudget struct{ bytes, parts int }

func (b *payloadInputBudget) addParts(items []ContentPart) bool {
	b.parts += len(items)
	if b.parts > maxPayloadEntries {
		return false
	}
	for _, part := range items {
		if mediaBodyIsInline(part) {
			return false
		}
		b.bytes += len(part.Type) + len(part.Content) + len(part.ID) + len(part.Name) +
			len(part.Arguments) + len(part.Result)
		if b.bytes > maxPayloadInputBytes {
			return false
		}
		for _, raw := range []json.RawMessage{part.Arguments, part.Result} {
			if !boundedJSONDepth(string(raw)) {
				return false
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
	return nil
}

func boundedToolJSON(raw string, cfg runtimeConfig) (string, error) {
	if raw == "" {
		return "", nil
	}
	if err := validateToolJSONInput(raw); err != nil {
		return "", err
	}
	if json.Valid([]byte(raw)) {
		return boundedJSON(json.RawMessage(raw), cfg.MaxContextLength())
	}
	// Tool inputs may be plain text. Encode them as a JSON string rather than
	// emitting malformed structured content or silently interpreting fragments.
	return boundedJSON(raw, cfg.MaxContextLength())
}

func recordPayloadDiagnostic(span trace.Span, err error) {
	if diagnostic, ok := errors.AsType[*ObservationError](err); ok {
		span.AddEvent("metry.observation.rejected", trace.WithAttributes(
			attribute.String("metry.diagnostic.field", diagnostic.Field),
			attribute.String("metry.diagnostic.rule", diagnostic.Rule),
		))
	}
}
