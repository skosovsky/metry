package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/skosovsky/metry/testutil"
)

func TestMetricsRegistry_NewHistogram_RecordsValueViaMetryNew(t *testing.T) {
	ctx := context.Background()
	metricMem := testutil.NewInMemoryMetricExporter()
	provider, err := New(
		ctx,
		WithServiceName("registry-e2e"),
		WithMetricExporter(testMetricExporter(metricMem)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	reg := NewMetricsRegistry(provider)
	hist, err := reg.NewHistogram("e2e_duration", []float64{1, 2})
	require.NoError(t, err)
	hist.Record(ctx, 1.5, Labels{"status": "ok"})
	require.NoError(t, provider.ForceFlush(ctx))

	rm := metricMem.LastResourceMetrics()
	require.NotNil(t, rm)
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "e2e_duration" {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.NotEmpty(t, h.DataPoints)
			assert.InDelta(t, 1.5, h.DataPoints[0].Sum, 1e-9)
			found = true
		}
	}
	require.True(t, found)
}

func TestMetricsRegistry_NewHistogram_RecordsValue(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	provider := &Provider{otelMeter: mp}

	reg := NewMetricsRegistry(provider)
	hist, err := reg.NewHistogram("agent_loop_duration", []float64{0.5, 1, 2})
	require.NoError(t, err)

	hist.Record(context.Background(), 1.2, Labels{"status": "ok"})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "agent_loop_duration" {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.NotEmpty(t, h.DataPoints)
			assert.InDelta(t, 1.2, h.DataPoints[0].Sum, 1e-9)
			val, ok := h.DataPoints[0].Attributes.Value("status")
			require.True(t, ok)
			assert.Equal(t, "ok", val.AsString())
			require.Len(t, h.DataPoints[0].Bounds, 3)
			assert.InDelta(t, 0.5, h.DataPoints[0].Bounds[0], 1e-9)
			assert.InDelta(t, 1.0, h.DataPoints[0].Bounds[1], 1e-9)
			assert.InDelta(t, 2.0, h.DataPoints[0].Bounds[2], 1e-9)
			return
		}
	}
	t.Fatal("metric agent_loop_duration not found")
}

func TestMetricsRegistry_DuplicateName_ReturnsError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})

	_, err := reg.NewHistogram("dup", nil)
	require.NoError(t, err)
	_, err = reg.NewHistogram("dup", nil)
	require.ErrorIs(t, err, ErrDuplicateMetric)
}

func TestMetricsRegistry_NotConfigured_ReturnsError(t *testing.T) {
	reg := NewMetricsRegistry(nil)
	_, err := reg.NewHistogram("x", nil)
	require.ErrorIs(t, err, ErrMetricsRegistryNotConfigured)
}

func TestMetricsRegistry_NilMeterProvider_NoPanic(t *testing.T) {
	reg := NewMetricsRegistry(&Provider{otelMeter: nil})
	require.NotNil(t, reg)
	_, err := reg.NewHistogram("x", nil)
	require.ErrorIs(t, err, ErrMetricsRegistryNotConfigured)
}

func TestHistogramMetric_Record_NilHist_NoOp(t *testing.T) {
	var h HistogramMetric
	assert.False(t, h.OK())
	require.NotPanics(t, func() {
		h.Record(context.Background(), 1.0, Labels{"k": "v"})
	})
}

func TestHistogramMetric_OK_ReportsRegistration(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})

	hist, err := reg.NewHistogram("ok_test", nil)
	require.NoError(t, err)
	assert.True(t, hist.OK())
}

func TestLabelsToAttributes_SkipsEmptyKeyAndValue(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})

	hist, err := reg.NewHistogram("labels_filter_test", nil)
	require.NoError(t, err)

	hist.Record(context.Background(), 1.0, Labels{"": "x", "status": "", "ok": "yes"})
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "labels_filter_test" {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.NotEmpty(t, h.DataPoints)
			attrs := h.DataPoints[0].Attributes
			_, hasEmptyKey := attrs.Value("")
			_, hasStatus := attrs.Value("status")
			val, hasOK := attrs.Value("ok")
			require.False(t, hasEmptyKey)
			require.False(t, hasStatus)
			require.True(t, hasOK)
			assert.Equal(t, "yes", val.AsString())
			return
		}
	}
	t.Fatal("metric labels_filter_test not found")
}

func TestMetricsRegistry_NewCounter_AddsValue(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})

	cnt, err := reg.NewCounter("agent_loop_steps_total")
	require.NoError(t, err)

	cnt.Add(context.Background(), 3, Labels{"status": "ok"})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "agent_loop_steps_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.NotEmpty(t, sum.DataPoints)
			assert.Equal(t, int64(3), sum.DataPoints[0].Value)
			val, ok := sum.DataPoints[0].Attributes.Value("status")
			require.True(t, ok)
			assert.Equal(t, "ok", val.AsString())
			return
		}
	}
	t.Fatal("metric agent_loop_steps_total not found")
}

func TestMetricsRegistry_NewGauge_RecordsValue(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})

	gauge, err := reg.NewGauge("queue_depth")
	require.NoError(t, err)

	gauge.Record(context.Background(), 42.5, Labels{"queue": "jobs"})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "queue_depth" {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[float64])
			require.True(t, ok)
			require.NotEmpty(t, g.DataPoints)
			assert.InDelta(t, 42.5, g.DataPoints[0].Value, 1e-9)
			val, ok := g.DataPoints[0].Attributes.Value("queue")
			require.True(t, ok)
			assert.Equal(t, "jobs", val.AsString())
			return
		}
	}
	t.Fatal("metric queue_depth not found")
}

func TestMetricsRegistry_DuplicateName_CrossType(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})

	_, err := reg.NewHistogram("dup", nil)
	require.NoError(t, err)
	_, err = reg.NewCounter("dup")
	require.ErrorIs(t, err, ErrDuplicateMetric)
}

func TestMetricsRegistry_DuplicateName_CounterThenGauge(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	reg := NewMetricsRegistry(&Provider{otelMeter: mp})

	_, err := reg.NewCounter("dup_gauge")
	require.NoError(t, err)
	_, err = reg.NewGauge("dup_gauge")
	require.ErrorIs(t, err, ErrDuplicateMetric)
}

func TestCounterMetric_Add_NilCounter_NoOp(t *testing.T) {
	var c CounterMetric
	assert.False(t, c.OK())
	require.NotPanics(t, func() {
		c.Add(context.Background(), 1, Labels{"k": "v"})
	})
}

func TestGaugeMetric_Record_NilGauge_NoOp(t *testing.T) {
	var g GaugeMetric
	assert.False(t, g.OK())
	require.NotPanics(t, func() {
		g.Record(context.Background(), 1.0, Labels{"k": "v"})
	})
}
