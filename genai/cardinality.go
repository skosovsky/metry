package genai

import (
	"sync"

	"go.opentelemetry.io/otel/attribute"
)

const (
	usageSourceKey            = "metry.usage.source"
	overflowKey               = "metry.dimension.overflow"
	provenanceCount           = 3
	totalTokenTypeCount       = 2
	componentTokenTypeCount   = 3
	semanticDimensionCapacity = 3
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
		if key == TokenType || key == usageSourceKey || key == ToolMetricLabelStatus ||
			(instrument == CostMetricName && key == CostCurrency) {
			normalized = append(normalized, attr)
			semantic = append(semantic, attr)
			continue
		}
		value := attr.Value.Emit()
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
	case TokenUsageMetricName:
		return totalTokenTypeCount * provenanceCount
	case TokenComponentUsageMetricName:
		return componentTokenTypeCount * provenanceCount
	case CostMetricName, VideoSecondsMetricName, VideoFramesMetricName,
		ToolDurationMetricName, StreamingTPSMetricName, StreamingTBTMetricName:
		return provenanceCount
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
