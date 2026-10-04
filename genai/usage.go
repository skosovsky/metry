package genai

import (
	"errors"
	"math"
)

// Source describes how a caller obtained a measurement.
type Source string

const (
	SourceMeasured  Source = "measured"
	SourceProvider  Source = "provider_reported"
	SourceEstimated Source = "estimated"
	SourceUnknown   Source = "unknown"
)

// Unit describes a measurement's physical or accounting unit.
type Unit string

const (
	UnitTokens   Unit = "token"
	UnitSeconds  Unit = "s"
	UnitImages   Unit = "image"
	UnitFrames   Unit = "frame"
	UnitCurrency Unit = "currency"
)

// Measurement distinguishes a known zero from absence. Its zero value is absent.
// SourceUnknown is valid only for an absent measurement.
type Measurement[T int | float64] struct {
	Value   T
	Present bool
	Source  Source
	Unit    Unit
}

// Observe constructs an explicitly present caller-owned measurement.
func Observe[T int | float64](value T, unit Unit, source Source) Measurement[T] {
	return Measurement[T]{Value: value, Present: true, Source: source, Unit: unit}
}

// Usage contains inclusive totals and separately reported, potentially overlapping
// components. Missing totals are never reconstructed from partial components.
// Cost and currency are supplied by the caller; metry never looks up prices.
type Usage struct {
	InputTokens              Measurement[int]
	OutputTokens             Measurement[int]
	CacheCreationInputTokens Measurement[int]
	CacheReadInputTokens     Measurement[int]
	ReasoningOutputTokens    Measurement[int]
	Cost                     Measurement[float64]
	Currency                 string
	AudioSeconds             Measurement[float64]
	ImageCount               Measurement[int]
	VideoSeconds             Measurement[float64]
	VideoFrames              Measurement[int]
	Purpose                  string
	Mapping                  string
	PriceReference           string
}

// ErrInvalidObservation is a validation error that never contains raw values.
var ErrInvalidObservation = errors.New("genai: invalid observation")

// ObservationError identifies a bounded field and rule, without echoing input.
type ObservationError struct {
	Field string
	Rule  string
}

func (e *ObservationError) Error() string { return "genai: " + e.Field + ": " + e.Rule }
func (e *ObservationError) Unwrap() error { return ErrInvalidObservation }

func invalidObservation(field, rule string) error {
	return &ObservationError{Field: field, Rule: rule}
}

// Validate checks the complete observation before any instrumentation side effect.
func (u Usage) Validate() error {
	for _, item := range []struct {
		field string
		value Measurement[int]
		unit  Unit
	}{
		{"input_tokens", u.InputTokens, UnitTokens},
		{"output_tokens", u.OutputTokens, UnitTokens},
		{"cache_creation", u.CacheCreationInputTokens, UnitTokens},
		{"cache_read", u.CacheReadInputTokens, UnitTokens},
		{reasoningLabel, u.ReasoningOutputTokens, UnitTokens},
		{"images", u.ImageCount, UnitImages},
		{"video_frames", u.VideoFrames, UnitFrames},
	} {
		if err := validateMeasurement(item.field, item.value, item.unit); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		field string
		value Measurement[float64]
		unit  Unit
	}{
		{"cost", u.Cost, UnitCurrency},
		{"audio_seconds", u.AudioSeconds, UnitSeconds},
		{"video_seconds", u.VideoSeconds, UnitSeconds},
	} {
		if err := validateMeasurement(item.field, item.value, item.unit); err != nil {
			return err
		}
	}
	for _, pair := range []struct {
		field            string
		component, total Measurement[int]
	}{
		{"cache_creation", u.CacheCreationInputTokens, u.InputTokens},
		{"cache_read", u.CacheReadInputTokens, u.InputTokens},
		{reasoningLabel, u.ReasoningOutputTokens, u.OutputTokens},
	} {
		if pair.component.Present && pair.total.Present && pair.component.Value > pair.total.Value {
			return invalidObservation(pair.field, "exceeds_total")
		}
	}
	if u.Cost.Present && (len(u.Currency) == 0 || len(u.Currency) > 16 || !asciiCurrency(u.Currency)) {
		return invalidObservation("currency", "required_currency_code")
	}
	if len(u.PriceReference) > 256 || len(u.Purpose) > 80 {
		return invalidObservation("usage_metadata", "too_large")
	}
	if u.Mapping != "" && u.Mapping != profileInclusiveMapping {
		return invalidObservation("mapping", "unsupported")
	}
	return nil
}

func (t *Tracker) validateUsage(usage Usage) error {
	if err := usage.Validate(); err != nil {
		return err
	}
	if usage.Cost.Present {
		if _, allowed := t.dimensions.allowed[CostCurrency][usage.Currency]; !allowed {
			return invalidObservation("currency", "unmapped")
		}
	}
	return nil
}

func asciiCurrency(value string) bool {
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return false
		}
	}
	return true
}

func validateMeasurement[T int | float64](field string, value Measurement[T], unit Unit) error {
	if !value.Present {
		if value.Value != 0 || value.Unit != "" || (value.Source != "" && value.Source != SourceUnknown) {
			return invalidObservation(field, "contradictory_absence")
		}
		return nil
	}
	if value.Unit != unit {
		return invalidObservation(field, "incompatible_unit")
	}
	if value.Source != SourceMeasured && value.Source != SourceProvider && value.Source != SourceEstimated {
		return invalidObservation(field, "invalid_source")
	}
	number := float64(value.Value)
	if number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return invalidObservation(field, "invalid_value")
	}
	return nil
}
