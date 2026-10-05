package genai

import (
	"sync"

	"go.opentelemetry.io/otel/attribute"
)

const (
	usageSourceKey            = "metry.usage.source"
	overflowKey               = "metry.dimension.overflow"
	provenanceCount           = 3
	componentTokenTypeCount   = 3
	semanticDimensionCapacity = 3
	outcomeCount              = 3
	invocationBoundaryCount   = 3 // local agent, remote agent, local workflow
)

// metricDimensions normalizes inputs only; the metric SDK owns aggregation/export.
// Its combination cache is bounded per instrument and per tracker.
type metricDimensions struct {
	mu           sync.Mutex
	budget       int
	allowed      map[string]map[string]struct{}
	combinations map[string]map[attribute.Distinct]struct{}
}

func newMetricDimensions(profile Profile) *metricDimensions {
	allowed := make(map[string]map[string]struct{}, len(profile.Dimensions))
	for _, dimension := range profile.Dimensions {
		values := make(map[string]struct{}, len(dimension.Values))
		for _, value := range dimension.Values {
			values[value] = struct{}{}
		}
		allowed[dimension.Key] = values
	}
	return &metricDimensions{
		mu:           sync.Mutex{},
		budget:       profile.SeriesBudget,
		allowed:      allowed,
		combinations: make(map[string]map[attribute.Distinct]struct{}),
	}
}

func (d *metricDimensions) normalize(instrument string, attrs []attribute.KeyValue) []attribute.KeyValue {
	normalized := make([]attribute.KeyValue, 0, len(attrs))
	semantic := make([]attribute.KeyValue, 0, semanticDimensionCapacity)
	for _, attr := range attrs {
		key := string(attr.Key)
		if key == DerivedCalculationKey || key == InvocationKindKey || key == ExecutionLocalityKey ||
			key == TokenType || key == usageSourceKey || key == OperationStatus ||
			(instrument == CostMetricName && key == CostCurrency) {
			normalized = append(normalized, attr)
			semantic = append(semantic, attr)
			continue
		}
		value := attr.Value.String()
		if _, ok := d.allowed[key][value]; !ok {
			if attr.Value.Type() == attribute.INT64 {
				attr = attr.Key.Int(0)
			} else {
				attr = attr.Key.String(unknownProviderName)
			}
		}
		normalized = append(normalized, attr)
	}
	set := attribute.NewSet(normalized...)
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := d.combinations[instrument]
	if seen == nil {
		seen = make(map[attribute.Distinct]struct{})
		d.combinations[instrument] = seen
	}
	if _, exists := seen[set.Equivalent()]; exists {
		return normalized
	}
	reserve := overflowReserve(instrument)
	if instrument == CostMetricName {
		reserve = max(1, len(d.allowed[CostCurrency])) * provenanceCount
	}
	if len(seen) < d.budget-reserve {
		seen[set.Equivalent()] = struct{}{}
		return normalized
	}
	return append(semantic, attribute.Bool(overflowKey, true))
}

func overflowReserve(instrument string) int {
	switch instrument {
	case InputTokenUsageMetricName, OutputTokenUsageMetricName:
		return provenanceCount
	case TokenComponentUsageMetricName:
		return componentTokenTypeCount * provenanceCount
	case CostMetricName, VideoSecondsMetricName, VideoFramesMetricName,
		DerivedOutputTokensPerSecondMetricName, DerivedMeanOutputTokenIntervalMetricName:
		return provenanceCount
	case ToolDurationMetricName, OperationDurationMetricName, ObservedOperationDurationMetricName:
		return outcomeCount // Preserve success/error/unknown in separate overflow series.
	case InvocationDurationMetricName:
		return invocationBoundaryCount * outcomeCount
	default:
		return 1
	}
}

func (t *Tracker) boundedMetricAttributes(instrument string, attrs []attribute.KeyValue) []attribute.KeyValue {
	if t.dimensions == nil {
		return attrs
	}
	return t.dimensions.normalize(instrument, attrs)
}
