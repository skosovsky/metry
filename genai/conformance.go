package genai

import (
	"slices"
	"strconv"
)

// AdapterDeclaration claims ownership of configured telemetry boundaries.
// Concrete adapters translate their own types; metry does not execute them.
type AdapterDeclaration struct {
	ProfileID  string
	Mapping    string
	Boundaries []string
	Units      []FieldUnitDeclaration
}

// FieldUnitDeclaration binds a measurement field to its canonical unit.
// An empty adapter Units slice inherits the profile catalogue; a nonempty slice
// is an exhaustive declaration for that adapter's exported measurement fields.
type FieldUnitDeclaration struct {
	Field string
	Unit  Unit
}

// ObservedBoundary is an exporter-independent fixture result. A fixture should
// derive it from exported telemetry rather than from its recording inputs.
type ObservedBoundary struct {
	Boundary   string
	Operation  string
	Fields     []ProfileField
	Usage      Usage
	Payloads   []PayloadEvidence
	Series     []InstrumentSeries
	Accounting *AccountingEvidence
	Enums      []ObservedEnum
	Events     []ObservedEvent
}

// InstrumentSeries counts all exported attribute combinations for an instrument.
type InstrumentSeries struct {
	Instrument string
	Count      int
}

// ConformanceFixture runs a caller-provided scenario against a configured runtime.
type ConformanceFixture func() ([]ObservedBoundary, error)

// ConformanceCheck uses categorical actual results, never raw fixture values.
type ConformanceCheck struct {
	Fixture    int
	Instrument string
	Rule       string
	Expected   string
	Actual     string
	Passed     bool
}

// ConformanceReport covers only declared fixture observations, not arbitrary host
// writes or exporter code outside the controlled instrumentation composition.
type ConformanceReport struct {
	Checks []ConformanceCheck
}

// Passed reports whether every executed check passed.
func (r *ConformanceReport) Passed() bool {
	if len(r.Checks) == 0 {
		return false
	}
	for _, check := range r.Checks {
		if !check.Passed {
			return false
		}
	}
	return true
}

// CheckConformance validates the composition before running any fixture.
// Configuration failures return typed errors; fixture failures become safe report
// entries so their original error messages cannot disclose payloads.
func CheckConformance(
	contract Contract,
	declarations []AdapterDeclaration,
	fixtures ...ConformanceFixture,
) (ConformanceReport, error) {
	if err := validateComposition(contract, declarations); err != nil {
		return ConformanceReport{Checks: nil}, err
	}
	if len(fixtures) == 0 || len(fixtures) > maxProfileEntries {
		return ConformanceReport{Checks: nil}, invalidObservation("fixtures", "too_large")
	}
	report := ConformanceReport{Checks: nil}
	for index, fixture := range fixtures {
		if fixture == nil {
			return ConformanceReport{Checks: nil}, invalidObservation("fixture", "missing")
		}
		observations, err := fixture()
		if err != nil {
			report.add(index, "", "execution", "success", "failed", false)
			continue
		}
		if len(observations) == 0 || len(observations) > maxProfileEntries {
			report.add(index, "", "observation_count", "bounded_nonempty", conformanceInvalid, false)
			continue
		}
		for _, observation := range observations {
			owned := ownsBoundary(declarations, observation.Boundary)
			report.add(index, "", "ownership", "declared_owner", categorical(owned), owned)
			report.checkObservation(index, contract, observation)
			report.checkEvidence(index, contract, declarations, observation)
		}
	}
	return report, nil
}

func ownsBoundary(declarations []AdapterDeclaration, boundary string) bool {
	for _, declaration := range declarations {
		if slices.Contains(declaration.Boundaries, boundary) {
			return true
		}
	}
	return false
}

func validateComposition(contract Contract, declarations []AdapterDeclaration) error {
	if err := contract.Validate(); err != nil {
		return err
	}
	profile := contract.Profile()
	if len(declarations) > maxProfileEntries {
		return invalidObservation("adapters", "too_large")
	}
	owners := make(map[string]struct{})
	for _, declaration := range declarations {
		if declaration.ProfileID != profile.ID {
			return invalidObservation("adapter_profile", "incompatible")
		}
		if declaration.Mapping != profile.Mapping {
			return invalidObservation("adapter_mapping", "incompatible")
		}
		if len(declaration.Boundaries) > maxProfileEntries || len(declaration.Units) > maxProfileEntries {
			return invalidObservation("adapter", "too_large")
		}
		if err := validateFieldUnits(profile, declaration.Units); err != nil {
			return err
		}
		for _, boundary := range declaration.Boundaries {
			if _, valid := findBoundary(profile, boundary); !valid {
				return invalidObservation("adapter_boundary", "unsupported")
			}
			if _, conflict := owners[boundary]; conflict {
				return invalidObservation("adapter_boundary", "ownership_conflict")
			}
			owners[boundary] = struct{}{}
		}
	}
	return nil
}

func findBoundary(profile Profile, name string) (Boundary, bool) {
	for _, boundary := range profile.Boundaries {
		if boundary.Name == name {
			return boundary, true
		}
	}
	return Boundary{Name: "", Operation: "", Accounting: false}, false
}

func (r *ConformanceReport) add(index int, instrument, rule, expected, actual string, passed bool) {
	r.Checks = append(
		r.Checks,
		ConformanceCheck{
			Fixture:    index,
			Instrument: instrument,
			Rule:       rule,
			Expected:   expected,
			Actual:     actual,
			Passed:     passed,
		},
	)
}

func (r *ConformanceReport) checkObservation(index int, contract Contract, observed ObservedBoundary) {
	profile := contract.Profile()
	boundary, known := findBoundary(profile, observed.Boundary)
	r.add(index, "", "boundary", "declared", categorical(known), known)
	if !known {
		return
	}
	operationValid := observed.Operation == boundary.Operation
	r.add(index, "", "operation", "declared", categorical(operationValid), operationValid)
	if len(observed.Fields) > maxProfileEntries || len(observed.Payloads) > maxProfileEntries ||
		len(observed.Series) > maxProfileEntries {
		r.add(index, "", "observation_size", "bounded", conformanceInvalid, false)
		return
	}
	fieldValid := compatibleObservedFields(profile.Fields, observed.Fields)
	r.add(index, "", "fields", "compatible", categorical(fieldValid), fieldValid)
	usageValid := observed.Usage.Validate() == nil
	r.add(index, "", "usage", "valid", categorical(usageValid), usageValid)
	for _, payload := range observed.Payloads {
		valid := validPayloadEvidence(contract, payload)
		r.add(index, "", "content_encoding", "bounded_json", categorical(valid), valid)
	}
	for _, series := range observed.Series {
		instrument := safeInstrument(series.Instrument)
		valid := instrument != "" && series.Count >= 0 && series.Count <= profile.SeriesBudget
		actual := conformanceInvalid
		if valid {
			actual = strconv.Itoa(series.Count)
		}
		r.add(index, instrument, "cardinality", strconv.Itoa(profile.SeriesBudget), actual, valid)
	}
}

func compatibleObservedFields(expected, actual []ProfileField) bool {
	seen := make(map[string]struct{}, len(actual))
	for _, field := range actual {
		if _, duplicate := seen[field.Name]; duplicate {
			return false
		}
		seen[field.Name] = struct{}{}
		found := false
		for _, declared := range expected {
			if declared.Name == field.Name {
				found = declared.Unit == field.Unit && declared.Encoding == field.Encoding &&
					declared.Stability == field.Stability
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, field := range expected {
		if field.Required {
			if _, present := seen[field.Name]; !present {
				return false
			}
		}
	}
	return true
}

func categorical(valid bool) string {
	if valid {
		return "valid"
	}
	return conformanceInvalid
}

func safeInstrument(name string) string {
	if slices.Contains(
		[]string{
			TokenUsageMetricName,
			TokenComponentUsageMetricName,
			OperationDurationMetricName,
			CostMetricName,
			TTFTMetricName,
			StreamingTPSMetricName,
			StreamingTBTMetricName,
			VideoSecondsMetricName,
			VideoFramesMetricName,
			ToolDurationMetricName,
		},
		name,
	) {
		return name
	}
	return ""
}
