package genai

import (
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// decodePayloadJSON preserves numeric lexemes and accepts exactly one JSON value.
// Structural bounds are checked before decoding, and nodes are bounded before a
// caller may invoke a custom sanitizer.
func decodePayloadJSON(raw string) (any, error) {
	if len(raw) > maxPayloadInputBytes || !boundedJSONDepth(raw) {
		return nil, invalidObservation("payload_json", "input_bounds")
	}
	if !utf8.ValidString(raw) {
		return nil, invalidObservation("payload_json", "invalid_encoding")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, invalidObservation("payload_json", "invalid_encoding")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, invalidObservation("payload_json", "invalid_encoding")
	}
	remaining := maxPayloadEntries
	if !jsonNodesWithinBudget(value, &remaining) {
		return nil, invalidObservation("payload_json", "input_bounds")
	}
	return value, nil
}

func jsonNodesWithinBudget(value any, remaining *int) bool {
	*remaining--
	if *remaining < 0 {
		return false
	}
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			if !jsonNodesWithinBudget(child, remaining) {
				return false
			}
		}
	case map[string]any:
		for _, child := range typed {
			if !jsonNodesWithinBudget(child, remaining) {
				return false
			}
		}
	}
	return true
}
