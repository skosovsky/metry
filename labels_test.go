package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestLabelsOf_DefensiveCopy(t *testing.T) {
	src := map[string]string{"status": "ok"}
	labels := LabelsOf(src)
	src["status"] = "mutated"
	assert.Equal(t, "ok", labels["status"])
}

func TestLabelsOf_SkipsEmptyKeyAndValue(t *testing.T) {
	labels := LabelsOf(map[string]string{"": "x", "status": "", "ok": "yes"})
	require.Len(t, labels, 1)
	assert.Equal(t, "yes", labels["ok"])
}

func TestCopyLabels_SkipsEmptyKeyAndValue(t *testing.T) {
	copied := copyLabels(Labels{"": "x", "status": "", "ok": "yes"})
	require.Len(t, copied, 1)
	assert.Equal(t, "yes", copied["ok"])
}

func TestCopyLabels_DefensiveCopy(t *testing.T) {
	src := Labels{"status": "ok"}
	copied := copyLabels(src)
	copied["status"] = "mutated"
	assert.Equal(t, "ok", src["status"])
}

func TestHistogramMetric_Record_LabelMutationDoesNotAffectRecordedAttrs(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	provider := &Provider{otelMeter: mp}

	reg := NewMetricsRegistry(provider)
	hist, err := reg.NewHistogram("label_mutation", []float64{1})
	require.NoError(t, err)

	labels := Labels{"status": "ok"}
	hist.Record(context.Background(), 1.0, labels)
	labels["status"] = "mutated"

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "label_mutation" {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.NotEmpty(t, h.DataPoints)
			attrs := h.DataPoints[0].Attributes
			val, ok := attrs.Value("status")
			require.True(t, ok)
			assert.Equal(t, "ok", val.AsString())
			return
		}
	}
	t.Fatal("metric label_mutation not found")
}
