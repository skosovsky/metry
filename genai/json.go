package genai

import (
	"bytes"
	"encoding/json"
	"io"
)

type jsonStringRef struct {
	current func() string
	shrink  func(overflow int) bool
}

type jsonStringRefOptions struct {
	deleteEmptyKeys map[string]struct{}
	skipKeys        map[string]struct{}
}

const (
	jsonStringRefsInitialCap  = 8
	jsonShrinkPassesPerString = 8
)

func normalizePayloadValue(value any, limit int) any {
	switch typed := value.(type) {
	case []GenAIContentPart:
		return normalizePayloadParts(typed, limit)
	case []GenAIMessage:
		return normalizePayloadMessages(typed, limit)
	default:
		return value
	}
}

func normalizePayloadJSON(raw []byte, limit int) (string, bool) {
	return normalizeStructuredJSON(raw, limit, jsonStringRefOptions{
		deleteEmptyKeys: map[string]struct{}{
			"content":       {},
			"id":            {},
			"name":          {},
			"finish_reason": {},
		},
		skipKeys: map[string]struct{}{
			"role":          {},
			"type":          {},
			"finish_reason": {},
		},
	})
}

func normalizeToolJSON(raw string, limit int) (string, bool) {
	return normalizeStructuredJSON([]byte(raw), limit, jsonStringRefOptions{
		deleteEmptyKeys: nil,
		skipKeys:        nil,
	})
}

//nolint:gocognit // Structured JSON walk: decode, marshal, iterative string shrinking; splitting would obscure control flow.
func normalizeStructuredJSON(raw []byte, limit int, opts jsonStringRefOptions) (string, bool) {
	if len(raw) == 0 || limit <= 0 {
		return "", false
	}

	value, err := decodeJSON(raw)
	if err != nil {
		return "", false
	}

	current, err := marshalStructuredJSON(value)
	if err != nil || current == "null" || current == "[]" {
		return "", false
	}
	if len(current) <= limit {
		return current, true
	}

	refs := collectJSONStringRefs(value, opts)
	if len(refs) == 0 {
		return "", false
	}

	for range len(refs) * jsonShrinkPassesPerString {
		overflow := len(current) - limit
		ref := longestJSONStringRef(refs)
		if ref == nil || !ref.shrink(overflow) {
			break
		}

		current, err = marshalStructuredJSON(value)
		if err != nil {
			return "", false
		}
		if len(current) <= limit {
			return current, true
		}
	}

	for len(current) > limit {
		ref := longestJSONStringRef(refs)
		if ref == nil || !ref.shrink(len(current)-limit+1) {
			return "", false
		}

		current, err = marshalStructuredJSON(value)
		if err != nil {
			return "", false
		}
	}

	return current, true
}

func normalizePayloadMessages(messages []GenAIMessage, limit int) []GenAIMessage {
	if len(messages) == 0 {
		return nil
	}

	normalized := make([]GenAIMessage, len(messages))
	for index, message := range messages {
		normalized[index] = GenAIMessage{
			Role:         message.Role,
			Parts:        normalizePayloadParts(message.Parts, limit),
			FinishReason: message.FinishReason,
		}
	}
	return normalized
}

func normalizePayloadParts(parts []GenAIContentPart, limit int) []GenAIContentPart {
	if len(parts) == 0 {
		return nil
	}

	normalized := make([]GenAIContentPart, len(parts))
	for index, part := range parts {
		normalized[index] = GenAIContentPart{
			Type:      part.Type,
			Content:   part.Content,
			ID:        part.ID,
			Name:      part.Name,
			Arguments: nil,
			Result:    nil,
		}
		if value, ok := normalizeToolJSON(string(part.Arguments), limit); ok {
			normalized[index].Arguments = json.RawMessage(value)
		}
		if value, ok := normalizeToolJSON(string(part.Result), limit); ok {
			normalized[index].Result = json.RawMessage(value)
		}
	}
	return normalized
}

func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	return value, nil
}

func marshalStructuredJSON(value any) (string, error) {
	buf, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func collectJSONStringRefs(value any, opts jsonStringRefOptions) []jsonStringRef {
	refs := make([]jsonStringRef, 0, jsonStringRefsInitialCap)
	collectJSONStringRefsInto(&refs, value, opts)
	return refs
}

//nolint:gocognit // Recursive JSON tree walk over map/slice shapes; shared helper for string ref collection.
func collectJSONStringRefsInto(refs *[]jsonStringRef, value any, opts jsonStringRefOptions) {
	switch node := value.(type) {
	case map[string]any:
		for key := range node {
			child := node[key]
			if text, ok := child.(string); ok {
				if _, skip := opts.skipKeys[key]; skip {
					continue
				}
				deleteOnEmpty := false
				if _, ok := opts.deleteEmptyKeys[key]; ok {
					deleteOnEmpty = true
				}
				*refs = append(*refs, objectJSONStringRef(node, key, text, deleteOnEmpty))
				continue
			}
			collectJSONStringRefsInto(refs, child, opts)
		}
	case []any:
		for index := range node {
			child := node[index]
			if text, ok := child.(string); ok {
				*refs = append(*refs, sliceJSONStringRef(node, index, text))
				continue
			}
			collectJSONStringRefsInto(refs, child, opts)
		}
	}
}

func objectJSONStringRef(node map[string]any, key string, text string, deleteOnEmpty bool) jsonStringRef {
	_ = text
	return jsonStringRef{
		current: func() string {
			value, _ := node[key].(string)
			return value
		},
		shrink: func(overflow int) bool {
			current, ok := node[key].(string)
			if !ok {
				return false
			}
			next, shrunk := shrinkJSONStringLeaf(current, overflow)
			if !shrunk {
				return false
			}
			if next == "" && deleteOnEmpty {
				delete(node, key)
				return true
			}
			node[key] = next
			return true
		},
	}
}

func sliceJSONStringRef(node []any, index int, text string) jsonStringRef {
	_ = text
	return jsonStringRef{
		current: func() string {
			value, _ := node[index].(string)
			return value
		},
		shrink: func(overflow int) bool {
			current, ok := node[index].(string)
			if !ok {
				return false
			}
			next, shrunk := shrinkJSONStringLeaf(current, overflow)
			if !shrunk {
				return false
			}
			node[index] = next
			return true
		},
	}
}

func longestJSONStringRef(refs []jsonStringRef) *jsonStringRef {
	var selected *jsonStringRef
	longest := 0
	for index := range refs {
		currentLen := len(refs[index].current())
		if currentLen > longest {
			longest = currentLen
			selected = &refs[index]
		}
	}
	return selected
}

func shrinkJSONStringLeaf(current string, overflow int) (string, bool) {
	if current == "" {
		return "", false
	}

	target := len(current) - overflow
	if target >= len(current) {
		target = len(current) - 1
	}
	if target < 0 {
		target = 0
	}

	next := truncateContextWithLimit(current, target)
	if len(next) >= len(current) && len(current) > 0 {
		next = truncateContextWithLimit(current, len(current)-1)
	}
	if len(next) >= len(current) {
		return "", false
	}
	return next, true
}
