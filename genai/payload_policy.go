package genai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	redactedPayloadText = "[redacted]"
	redactedPayloadJSON = `{"redacted":true}`
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

type redactPayloadPolicy struct{}

// RedactPayloadPolicy returns the default safe policy: structure is preserved, content is redacted.
func RedactPayloadPolicy() PayloadPolicy {
	return redactPayloadPolicy{}
}

func (redactPayloadPolicy) SanitizePayload(payload Payload) Payload {
	return sanitizePayload(payload, redactContentPart)
}

func (redactPayloadPolicy) SanitizeToolCall(call ToolCall) ToolCall {
	if call.Arguments != "" {
		call.Arguments = sanitizeJSONString(call.Arguments, redactJSONLeaf)
	}
	return call
}

func (redactPayloadPolicy) SanitizeToolResult(result ToolResult) ToolResult {
	if result.Result != "" {
		result.Result = sanitizeJSONString(result.Result, redactJSONLeaf)
	}
	return result
}

type hashPayloadPolicy struct{}

// HashPayloadPolicy returns a policy that replaces content with deterministic fingerprints.
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
			Role:         msg.Role,
			Parts:        sanitizeContentParts(msg.Parts, sanitizePart),
			FinishReason: msg.FinishReason,
		}
	}
	return out
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
	if part.Content != "" {
		part.Content = redactedPayloadText
	}
	if len(part.Arguments) > 0 {
		part.Arguments = sanitizeJSONRaw(part.Arguments, redactJSONLeaf)
	}
	if len(part.Result) > 0 {
		part.Result = sanitizeJSONRaw(part.Result, redactJSONLeaf)
	}
	return part
}

func hashContentPart(part ContentPart) ContentPart {
	if part.Content != "" {
		part.Content = fingerprintPayloadLabel(part.Content)
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
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return redactedPayloadJSON
	}
	sanitized := sanitizeJSONValue(value, sanitizeLeaf)
	buf, err := json.Marshal(sanitized)
	if err != nil {
		return redactedPayloadJSON
	}
	return string(buf)
}

func sanitizeJSONValue(value any, sanitizeLeaf func(any) any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = sanitizeJSONValue(v, sanitizeLeaf)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = sanitizeJSONValue(v, sanitizeLeaf)
		}
		return out
	default:
		return sanitizeLeaf(typed)
	}
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
	payload := Payload{
		SystemInstructions: nil,
		InputMessages: []Message{{
			Role: "",
			Parts: []ContentPart{{
				Type:      "text",
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
