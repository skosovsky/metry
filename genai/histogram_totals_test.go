package genai

import (
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/skosovsky/metry/metrytest"
)

// Split inclusive token instruments carry no token-component dimension.
func testInt64HistogramSum(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	selector metrytest.MetricSelector,
) float64 {
	t.Helper()
	histogram := metrytest.FindInt64Histogram(t, metrics, selector)
	var total int64
	for _, point := range histogram.DataPoints {
		total += point.Sum
	}
	return float64(total)
}
