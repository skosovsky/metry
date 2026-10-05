package genai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func FuzzBoundedJSON(f *testing.F) {
	for _, seed := range []string{`[9007199254740993,1.000000000000000001e99]`, `{"n":9223372036854775807,"s":"<&\\\""}`, `[`, `1 2`, strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33), `"` + strings.Repeat("x", 65536) + `"`} {
		f.Add(seed, uint16(128))
	}
	f.Fuzz(func(t *testing.T, raw string, budget uint16) {
		// Arrange: do not process arbitrarily large generated inputs.
		if len(raw) > maxPayloadInputBytes {
			t.Skip()
		}
		limit := max(int(budget), len(truncatedJSONMarker))
		// Act.
		output, err := boundedJSON(json.RawMessage(raw), limit)
		// Assert.
		if err != nil {
			if len(err.Error()) > 160 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if output == "" {
			return
		}
		if len(output) > limit || !json.Valid([]byte(output)) {
			t.Fatal("invalid bounded output")
		}
		assertBoundedJSONPreserved(t, raw, output)
	})
}

func assertBoundedJSONPreserved(t *testing.T, raw, output string) {
	t.Helper()
	original, err := decodePayloadJSON(raw)
	if err != nil {
		t.Fatal("invalid input accepted")
	}
	actual, err := decodePayloadJSON(output)
	if err != nil {
		t.Fatal("invalid output accepted")
	}
	if reflect.DeepEqual(original, actual) {
		return
	}
	envelope, ok := actual.(map[string]any)
	if !ok || envelope["truncated"] != true {
		t.Fatal("missing truncation marker")
	}
	assertRetainedJSON(t, original, envelope["data"])
}

func assertRetainedJSON(t *testing.T, original, retained any) {
	t.Helper()
	if retained == nil {
		return
	}
	switch value := retained.(type) {
	case []any:
		input, ok := original.([]any)
		if !ok || len(value) > len(input) || !reflect.DeepEqual(input[:len(value)], value) {
			t.Fatal("retained array changed")
		}
	case map[string]any:
		input, ok := original.(map[string]any)
		if !ok {
			t.Fatal("retained object changed type")
		}
		for key, child := range value {
			before, exists := input[key]
			if !exists || !reflect.DeepEqual(before, child) {
				t.Fatal("retained object changed")
			}
		}
	default:
		t.Fatal("unexpected retained data")
	}
}

func FuzzPayloadSanitizer(f *testing.F) {
	for _, seed := range []string{`9007199254740993`, `1.234567890123456789e9999`, `{"n":9007199254740993}`, `[true,null,"<&"]`, `{`, `1 2`, strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		// Arrange.
		if len(raw) > maxPayloadInputBytes {
			t.Skip()
		}
		original, err := decodePayloadJSON(raw)
		// Act.
		output := sanitizeJSONString(raw, hashJSONLeaf)
		redacted := sanitizeJSONStringWithKeys(raw, redactJSONLeaf, payloadSchemaKeys([]string{"n"}))
		// Assert: sanitizers never export malformed or unbounded data.
		for _, value := range []string{output, redacted} {
			if len(value) > maxPayloadInputBytes || !json.Valid([]byte(value)) {
				t.Fatal("invalid sanitizer output")
			}
		}
		if err != nil {
			if output != redactedPayloadJSON {
				t.Fatal("invalid input not rejected")
			}
			return
		}
		if number, ok := original.(json.Number); ok {
			expected, _ := json.Marshal(fingerprintPayloadLabel(number.String()))
			if output != string(expected) {
				t.Fatal("numeric fingerprint lost precision")
			}
		}
	})
}
