package traceutil

import (
	"bytes"
	"encoding/json"
)

// MarshalToken encodes trace-only JSON without HTML escaping. Tokens are base64
// encoded, so HTML escaping only inflates valid W3C tracestate values.
func MarshalToken(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
