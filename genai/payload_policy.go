package genai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

const (
	redactedPayloadText  = "[redacted]"
	redactedPayloadJSON  = `{"redacted":true}`
	maxPayloadSchemaKeys = 128
	maxPayloadJSONDepth  = 32
	payloadTextType      = "text"
	payloadImageType     = "image"
	payloadImageURLType  = "image_url"
	payloadAudioType     = "audio"
	payloadVideoType     = "video"
	payloadFileType      = "file"
	payloadToolCallType  = "tool_call"
	payloadStopFinish    = "stop"
	payloadAssistantRole = "assistant"
	payloadUserRole      = "user"
	reasoningLabel       = "reasoning"
)

// PayloadPolicy sanitizes GenAI payload before it is exported as span attributes.
type PayloadPolicy interface {
	SanitizePayload(payload Payload) Payload
	SanitizeToolCall(call ToolCall) ToolCall
	SanitizeToolResult(result ToolResult) ToolResult
}

type rawPayloadPolicy struct{}

// RawPayloadPolicy returns a policy that exports payloads as provided by the caller.
// Use this only when the caller has already handled privacy at the application boundary.
func RawPayloadPolicy() PayloadPolicy {
	return rawPayloadPolicy{}
}

func (rawPayloadPolicy) SanitizePayload(payload Payload) Payload { return payload }
func (rawPayloadPolicy) SanitizeToolCall(call ToolCall) ToolCall { return call }
func (rawPayloadPolicy) SanitizeToolResult(result ToolResult) ToolResult {
	return result
}

type redactPayloadPolicy struct{ keys map[string]struct{} }

// RedactPayloadPolicy preserves only explicitly allowlisted object keys.
// Unknown objects are redacted as a whole, including their dynamic keys.
func RedactPayloadPolicy(schemaKeys ...string) PayloadPolicy {
	return redactPayloadPolicy{keys: payloadSchemaKeys(schemaKeys)}
}

func (policy redactPayloadPolicy) SanitizePayload(payload Payload) Payload {
	return sanitizePayload(payload, func(part ContentPart) ContentPart {
		part = redactContentPart(part)
		if len(part.Arguments) > 0 {
			part.Arguments = json.RawMessage(
				sanitizeJSONStringWithKeys(string(part.Arguments), redactJSONLeaf, policy.keys),
			)
		}
		if len(part.Result) > 0 {
			part.Result = json.RawMessage(sanitizeJSONStringWithKeys(string(part.Result), redactJSONLeaf, policy.keys))
		}
		return part
	})
}

func (policy redactPayloadPolicy) SanitizeToolCall(call ToolCall) ToolCall {
	if call.Arguments != "" {
		call.Arguments = sanitizeJSONStringWithKeys(call.Arguments, redactJSONLeaf, policy.keys)
	}
	return call
}

func (policy redactPayloadPolicy) SanitizeToolResult(result ToolResult) ToolResult {
	if result.Result != "" {
		result.Result = sanitizeJSONStringWithKeys(result.Result, redactJSONLeaf, policy.keys)
	}
	return result
}

type hashPayloadPolicy struct{}

// HashPayloadPolicy returns a policy that replaces content with deterministic fingerprints.
// Object keys are ordered canonically; JSON numeric spelling is preserved.
// Numerically equivalent spellings (1, 1.0, 1e0) may have different fingerprints.
func HashPayloadPolicy() PayloadPolicy {
	return hashPayloadPolicy{}
}

func (hashPayloadPolicy) SanitizePayload(payload Payload) Payload {
	return sanitizePayload(payload, hashContentPart)
}

func (hashPayloadPolicy) SanitizeToolCall(call ToolCall) ToolCall {
	if call.Arguments != "" {
		call.Arguments = sanitizeJSONString(call.Arguments, hashJSONLeaf)
	}
	return call
}

func (hashPayloadPolicy) SanitizeToolResult(result ToolResult) ToolResult {
	if result.Result != "" {
		result.Result = sanitizeJSONString(result.Result, hashJSONLeaf)
	}
	return result
}

func sanitizePayload(payload Payload, sanitizePart func(ContentPart) ContentPart) Payload {
	return Payload{
		SystemInstructions: sanitizeContentParts(payload.SystemInstructions, sanitizePart),
		InputMessages:      sanitizeMessages(payload.InputMessages, sanitizePart),
		OutputMessages:     sanitizeMessages(payload.OutputMessages, sanitizePart),
	}
}

func sanitizeMessages(messages []Message, sanitizePart func(ContentPart) ContentPart) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, len(messages))
	for i, msg := range messages {
		out[i] = Message{
			Role: sanitizeMessageEnum(
				msg.Role,
				[]string{"system", payloadUserRole, payloadAssistantRole, ToolMetricLabelTool, "developer", "function"},
				sanitizePart,
			),
			Parts: sanitizeContentParts(msg.Parts, sanitizePart),
			FinishReason: sanitizeMessageEnum(
				msg.FinishReason,
				[]string{payloadStopFinish, "length", "tool_calls", "content_filter", OperationStatusError},
				sanitizePart,
			),
		}
	}
	return out
}

func sanitizeMessageEnum(value string, vocabulary []string, sanitizePart func(ContentPart) ContentPart) string {
	if value == "" || slices.Contains(vocabulary, value) {
		return value
	}
	return sanitizePart(
		ContentPart{Type: payloadTextType, Content: value, ID: "", Name: "", Arguments: nil, Result: nil},
	).Content
}

func safeContentType(value string, protect func(string) string) string {
	if value == "" ||
		slices.Contains(
			[]string{
				payloadTextType,
				payloadImageType,
				payloadImageURLType,
				payloadAudioType,
				payloadVideoType,
				payloadFileType,
				payloadToolCallType,
				"tool_result",
				reasoningLabel,
			},
			value,
		) {
		return value
	}
	return protect(value)
}

func redactLabel(value string) string {
	if value == "" {
		return ""
	}
	return redactedPayloadText
}

func sanitizeContentParts(parts []ContentPart, sanitizePart func(ContentPart) ContentPart) []ContentPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]ContentPart, len(parts))
	for i, part := range parts {
		out[i] = sanitizePart(part)
	}
	return out
}

func redactContentPart(part ContentPart) ContentPart {
	part.Type = safeContentType(part.Type, redactLabel)
	if part.Content != "" {
		part.Content = redactedPayloadText
	}
	if part.ID != "" {
		part.ID = redactedPayloadText
	}
	if part.Name != "" {
		part.Name = redactedPayloadText
	}
	return part
}

func hashContentPart(part ContentPart) ContentPart {
	part.Type = safeContentType(part.Type, fingerprintPayloadLabel)
	if part.Content != "" {
		part.Content = fingerprintPayloadLabel(part.Content)
	}
	if part.ID != "" {
		part.ID = fingerprintPayloadLabel(part.ID)
	}
	if part.Name != "" {
		part.Name = fingerprintPayloadLabel(part.Name)
	}
	if len(part.Arguments) > 0 {
		part.Arguments = fingerprintPayloadJSON(part.Arguments)
	}
	if len(part.Result) > 0 {
		part.Result = fingerprintPayloadJSON(part.Result)
	}
	return part
}

func fingerprintPayloadJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return sanitizeJSONRaw(raw, hashJSONLeaf)
}

func fingerprintPayloadLabel(value string) string {
	return "sha256:" + fingerprintPayloadText(value)
}

func fingerprintPayloadText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sanitizeJSONRaw(raw json.RawMessage, sanitizeLeaf func(any) any) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return json.RawMessage(sanitizeJSONString(string(raw), sanitizeLeaf))
}

func sanitizeJSONString(raw string, sanitizeLeaf func(any) any) string {
	return sanitizeJSONStringWithKeys(raw, sanitizeLeaf, nil)
}

func payloadSchemaKeys(keys []string) map[string]struct{} {
	allowed := make(map[string]struct{}, min(len(keys), maxPayloadSchemaKeys))
	for _, key := range keys {
		if len(allowed) >= maxPayloadSchemaKeys {
			break
		}
		if key != "" && len(key) <= 80 {
			allowed[key] = struct{}{}
		}
	}
	return allowed
}

func sanitizeJSONStringWithKeys(raw string, sanitizeLeaf func(any) any, keys map[string]struct{}) string {
	if len(raw) > maxPayloadInputBytes || !boundedJSONDepth(raw) {
		return redactedPayloadJSON
	}
	value, decodeErr := decodePayloadJSON(raw)
	if decodeErr != nil {
		return redactedPayloadJSON
	}
	sanitized := sanitizeJSONValue(value, sanitizeLeaf, keys)
	buf, err := json.Marshal(sanitized)
	if err != nil {
		return redactedPayloadJSON
	}
	return string(buf)
}

func sanitizeJSONValue(value any, sanitizeLeaf func(any) any, keys map[string]struct{}) any {
	switch typed := value.(type) {
	case map[string]any:
		for key := range typed {
			if _, allowed := keys[key]; !allowed {
				return sanitizeLeaf(typed)
			}
		}
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = sanitizeJSONValue(v, sanitizeLeaf, keys)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = sanitizeJSONValue(v, sanitizeLeaf, keys)
		}
		return out
	default:
		return sanitizeLeaf(typed)
	}
}

func boundedJSONDepth(raw string) bool {
	depth := 0
	inString, escaped := false, false
	for _, char := range raw {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxPayloadJSONDepth {
				return false
			}
		case '}', ']':
			depth--
		}
	}
	return true
}

func redactJSONLeaf(any) any {
	return redactedPayloadText
}

func hashJSONLeaf(value any) any {
	buf, err := json.Marshal(value)
	if err != nil {
		return fingerprintPayloadLabel(fmt.Sprint(value))
	}
	return fingerprintPayloadLabel(string(buf))
}

func sanitizeTextWithConfig(value string, cfg runtimeConfig) string {
	if value == "" {
		return ""
	}
	if len(value) > maxPayloadInputBytes {
		return redactedPayloadText
	}
	payload := Payload{
		SystemInstructions: nil,
		InputMessages: []Message{{
			Role: "",
			Parts: []ContentPart{{
				Type:      payloadTextType,
				Content:   value,
				ID:        "",
				Name:      "",
				Arguments: nil,
				Result:    nil,
			}},
			FinishReason: "",
		}},
		OutputMessages: nil,
	}
	sanitized := cfg.PayloadPolicy().SanitizePayload(payload)
	if len(sanitized.InputMessages) == 0 || len(sanitized.InputMessages[0].Parts) == 0 {
		return ""
	}
	return sanitized.InputMessages[0].Parts[0].Content
}
