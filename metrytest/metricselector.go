package metrytest

import (
	"testing"

	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// MetricAggregation identifies the concrete numeric aggregation expected by a selector.
type MetricAggregation uint8

const (
	// Int64Histogram selects metricdata.Histogram[int64].
	Int64Histogram MetricAggregation = iota + 1
	// Float64Histogram selects metricdata.Histogram[float64].
	Float64Histogram
	// Int64Sum selects metricdata.Sum[int64].
	Int64Sum
	// Float64Sum selects metricdata.Sum[float64].
	Float64Sum
	// Float64Gauge selects metricdata.Gauge[float64].
	Float64Gauge
)

// MetricSelector selects a name and optionally an exact instrumentation scope.
// A nil Scope searches all scopes but Find and summary helpers still require
// exactly one matching metric. Zero Aggregation uses the typed helper's type;
// AssertMetricAbsent uses zero to forbid every aggregation with the selected name.
type MetricSelector struct {
	Name        string
	Scope       *instrumentation.Scope
	Aggregation MetricAggregation
}

func validateSelector(t *testing.T, selector MetricSelector) {
	t.Helper()
	if selector.Name == "" || selector.Aggregation > Float64Gauge {
		t.Fatalf("invalid metric selector for %q", selector.Name)
	}
}

func matchesScope(actual instrumentation.Scope, expected *instrumentation.Scope) bool {
	return expected == nil || (actual.Name == expected.Name && actual.Version == expected.Version &&
		actual.SchemaURL == expected.SchemaURL && actual.Attributes.Equals(&expected.Attributes))
}

func aggregationOf(data metricdata.Aggregation) MetricAggregation {
	switch data.(type) {
	case metricdata.Histogram[int64]:
		return Int64Histogram
	case metricdata.Histogram[float64]:
		return Float64Histogram
	case metricdata.Sum[int64]:
		return Int64Sum
	case metricdata.Sum[float64]:
		return Float64Sum
	case metricdata.Gauge[float64]:
		return Float64Gauge
	default:
		return 0
	}
}

func selectMetric(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	selector MetricSelector,
	expected MetricAggregation,
) metricdata.Metrics {
	t.Helper()
	validateSelector(t, selector)
	if selector.Aggregation != 0 && selector.Aggregation != expected {
		t.Fatalf("metric %q selector aggregation disagrees with typed helper", selector.Name)
	}
	var found metricdata.Metrics
	count := 0
	for _, sm := range rm.ScopeMetrics {
		if !matchesScope(sm.Scope, selector.Scope) {
			continue
		}
		for _, candidate := range sm.Metrics {
			if candidate.Name == selector.Name {
				found = candidate
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("metric %q: expected exactly one name/scope match, got %d", selector.Name, count)
	}
	if aggregationOf(found.Data) != expected {
		t.Fatalf("metric %q has wrong aggregation type %T", selector.Name, found.Data)
	}
	return found
}
