package metrytest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/skosovsky/metry/metrytest"
)

func rmWithInt64Sum(name string, points ...metricdata.DataPoint[int64]) metricdata.ResourceMetrics {
	return metricdata.ResourceMetrics{
		ScopeMetrics: []metricdata.ScopeMetrics{{
			Metrics: []metricdata.Metrics{{
				Name: name,
				Data: metricdata.Sum[int64]{DataPoints: points},
			}},
		}},
	}
}

func rmWithGauge(name string, points ...metricdata.DataPoint[float64]) metricdata.ResourceMetrics {
	return metricdata.ResourceMetrics{
		ScopeMetrics: []metricdata.ScopeMetrics{{
			Metrics: []metricdata.Metrics{{
				Name: name,
				Data: metricdata.Gauge[float64]{DataPoints: points},
			}},
		}},
	}
}

func TestFindInt64Sum_Value(t *testing.T) {
	rm := rmWithInt64Sum("steps_total", metricdata.DataPoint[int64]{Value: 7})
	sum := metrytest.FindInt64Sum(t, rm, "steps_total")
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(7), sum.DataPoints[0].Value)
}

func TestInt64SumValue_SumsMultipleDataPoints(t *testing.T) {
	rm := rmWithInt64Sum("steps_total",
		metricdata.DataPoint[int64]{Value: 3},
		metricdata.DataPoint[int64]{Value: 4},
	)
	assert.Equal(t, int64(7), metrytest.Int64SumValue(t, rm, "steps_total"))
}

func TestFirstInt64SumAttr(t *testing.T) {
	rm := rmWithInt64Sum("steps_total", metricdata.DataPoint[int64]{
		Value: 1,
		Attributes: attribute.NewSet(
			attribute.String("status", "ok"),
		),
	})
	assert.Equal(t, "ok", metrytest.FirstInt64SumAttr(t, rm, "steps_total", "status"))
}

func TestFindGauge_Value(t *testing.T) {
	rm := rmWithGauge("queue_depth", metricdata.DataPoint[float64]{Value: 3})
	gauge := metrytest.FindGauge(t, rm, "queue_depth")
	require.Len(t, gauge.DataPoints, 1)
	assert.InDelta(t, 3, gauge.DataPoints[0].Value, 1e-9)
}

func TestGaugeFloat64Value(t *testing.T) {
	rm := rmWithGauge("queue_depth", metricdata.DataPoint[float64]{Value: 42.5})
	assert.InDelta(t, 42.5, metrytest.GaugeFloat64Value(t, rm, "queue_depth"), 1e-9)
}

func TestFirstGaugeAttr(t *testing.T) {
	rm := rmWithGauge("queue_depth", metricdata.DataPoint[float64]{
		Value: 3,
		Attributes: attribute.NewSet(
			attribute.String("queue", "jobs"),
		),
	})
	assert.Equal(t, "jobs", metrytest.FirstGaugeAttr(t, rm, "queue_depth", "queue"))
}
