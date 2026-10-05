package genai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/skosovsky/metry"
)

// Inputs are fixed: ordinary JSON and a 65,536-byte string,
// just above the default output budget. Benchmark output includes exact sizes.
func benchmarkPayloadInputs() []struct{ name, raw string } {
	return []struct{ name, raw string }{
		{"normal", `{"n":9007199254740993,"text":"ordinary application interaction"}`},
		{"boundary", `{"payload":"` + strings.Repeat("x", 65536) + `"}`},
	}
}

func BenchmarkPayloadEncode(b *testing.B) {
	for _, input := range benchmarkPayloadInputs() {
		b.Run(input.name, func(b *testing.B) {
			raw := json.RawMessage(input.raw)
			b.SetBytes(int64(len(raw)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := boundedJSON(raw, defaultMaxContextLength); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPayloadSanitize(b *testing.B) {
	for _, input := range benchmarkPayloadInputs() {
		b.Run(input.name, func(b *testing.B) {
			b.SetBytes(int64(len(input.raw)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = sanitizeJSONString(input.raw, hashJSONLeaf)
			}
		})
	}
}

func BenchmarkPayloadRecording(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			tracker := benchmarkRecordingTracker(b, enabled)
			for _, input := range benchmarkPayloadInputs() {
				b.Run(input.name, func(b *testing.B) { benchmarkRecordPayload(b, tracker, input.raw) })
			}
		})
	}
}

func benchmarkRecordingTracker(b *testing.B, enabled bool) *Tracker {
	b.Helper()
	provider, err := metry.New(
		context.Background(),
		metry.WithServiceName("payload-benchmark"),
		metry.WithSampler(metry.AlwaysSample()),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if shutdownErr := provider.Shutdown(context.Background()); shutdownErr != nil {
			b.Error(shutdownErr)
		}
	})
	options := []Option{WithProfile(testInstrumentationProfile())}
	if enabled {
		options = append(options, WithRawPayloads())
	}
	tracker, err := NewTrackerFromProvider(provider, options...)
	if err != nil {
		b.Fatal(err)
	}
	return tracker
}

func benchmarkRecordPayload(b *testing.B, tracker *Tracker, raw string) {
	b.Helper()
	payload := Payload{SystemInstructions: []ContentPart{{Arguments: json.RawMessage(raw)}}}
	meta := testMeta()
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := tracker.RecordInteraction(context.Background(), meta, payload, Usage{}); err != nil {
			b.Fatal(err)
		}
	}
}
