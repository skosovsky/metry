package metrytest_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/skosovsky/metry/metrytest"
)

func selectorFixture() metricdata.ResourceMetrics {
	return metricdata.ResourceMetrics{ScopeMetrics: []metricdata.ScopeMetrics{
		{
			Scope: instrumentation.Scope{
				Name:       "one",
				Version:    "v1",
				SchemaURL:  "schema1",
				Attributes: attribute.NewSet(attribute.String("env", "a")),
			},
			Metrics: []metricdata.Metrics{
				{Name: "shared", Data: metricdata.Sum[int64]{DataPoints: []metricdata.DataPoint[int64]{{Value: 7}}}},
			},
		},
		{
			Scope: instrumentation.Scope{Name: "two"},
			Metrics: []metricdata.Metrics{
				{Name: "shared", Data: metricdata.Sum[int64]{DataPoints: []metricdata.DataPoint[int64]{{Value: 9}}}},
			},
		},
	}}
}

func TestMetricSelectorExactScope(t *testing.T) {
	// Arrange.
	rm := selectorFixture()
	scope := rm.ScopeMetrics[0].Scope
	selector := metrytest.MetricSelector{Name: "shared", Scope: &scope, Aggregation: metrytest.Int64Sum}
	// Act.
	got := metrytest.Int64SumValue(t, rm, selector)
	// Assert.
	require.Equal(t, int64(7), got)
	metrytest.AssertMetricAbsent(
		t,
		rm,
		metrytest.MetricSelector{Name: "shared", Scope: &scope, Aggregation: metrytest.Float64Sum},
	)
}

func TestMetricSelectorRejectsInvalidSelection(t *testing.T) {
	if kind := os.Getenv("METRY_METRIC_FAILURE_CASE"); kind != "" {
		runInvalidSelection(t, kind)
		return
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	cases := []string{
		"ambiguous", "duplicate", "mixed_duplicate", "aggregation", "declared",
		"invalid", "version", "schema", "attributes", "absent",
	}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			// Arrange.
			cmd := exec.Command(binary, "-test.run=^TestMetricSelectorRejectsInvalidSelection$")
			cmd.Env = append(os.Environ(), "METRY_METRIC_FAILURE_CASE="+kind)
			// Act.
			output, err := cmd.CombinedOutput()
			// Assert.
			require.Error(t, err)
			require.Contains(t, string(output), "shared")
			if kind == "mixed_duplicate" {
				require.Contains(t, string(output), "expected exactly one name/scope match, got 2")
			}
		})
	}
}

func runInvalidSelection(t *testing.T, kind string) {
	t.Helper()
	rm := selectorFixture()
	scope := rm.ScopeMetrics[0].Scope
	selector := metrytest.MetricSelector{Name: "shared", Scope: &scope}
	switch kind {
	case "ambiguous":
		selector.Scope = nil
	case "duplicate":
		rm.ScopeMetrics[0].Metrics = append(rm.ScopeMetrics[0].Metrics, rm.ScopeMetrics[0].Metrics[0])
	case "mixed_duplicate":
		rm.ScopeMetrics[0].Metrics = append(
			rm.ScopeMetrics[0].Metrics,
			metricdata.Metrics{Name: "shared", Data: metricdata.Sum[float64]{}},
		)
	case "invalid":
		selector.Aggregation = metrytest.MetricAggregation(255)
	case "aggregation":
		rm.ScopeMetrics[0].Metrics[0].Data = metricdata.Sum[float64]{}
	case "declared":
		selector.Aggregation = metrytest.Float64Sum
	case "version":
		scope.Version = "v2"
	case "schema":
		scope.SchemaURL = "schema2"
	case "attributes":
		scope.Attributes = attribute.NewSet(attribute.String("env", "b"))
	case "absent":
		metrytest.AssertMetricAbsent(t, rm, selector)
		return
	}
	metrytest.Int64SumValue(t, rm, selector)
}
