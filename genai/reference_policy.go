package genai

import "slices"

// References preserve equality in non-raw modes, without exposing caller IDs.
// They are independently protected even when content recording is disabled.
func referenceWithConfig(value string, cfg recorderConfig) string {
	if value == "" {
		return ""
	}
	if len(value) > maxIdentityBytes {
		return redactedPayloadText
	}
	if _, raw := cfg.PayloadPolicy().(rawPayloadPolicy); raw {
		return value
	}
	return fingerprintPayloadLabel(value)
}

func toolNameWithConfig(value string, cfg recorderConfig) string {
	name := normalizeToolName(value)
	if _, raw := cfg.PayloadPolicy().(rawPayloadPolicy); raw {
		return name
	}
	for _, dimension := range cfg.profile.Dimensions {
		if dimension.Key == ToolName && slices.Contains(dimension.Values, name) {
			return name
		}
	}
	return unknownToolName
}

// Machine labels remain literal only when explicitly declared safe locally.
// Undeclared values keep stable correlation through a bounded fingerprint.
func metadataWithConfig(key, value string, cfg recorderConfig) string {
	if value == "" || value == unknownProviderName {
		return value
	}
	if len(value) > maxIdentityBytes {
		return redactedPayloadText
	}
	if _, raw := cfg.PayloadPolicy().(rawPayloadPolicy); raw {
		return value
	}
	for _, dimension := range cfg.profile.Dimensions {
		if dimension.Key == key && slices.Contains(dimension.Values, value) {
			return value
		}
	}
	if key == OperationName {
		for _, boundary := range Schema().Boundaries() {
			if boundary.Operation == value {
				return value
			}
		}
	}
	return referenceWithConfig(value, cfg)
}
