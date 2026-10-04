package genai

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func TestOperationOutcomeSurvivesSeriesOverflow(t *testing.T) {
	// Arrange: force overflow across all three independently reported outcomes.
	profile := testInstrumentationProfile()
	profile.SeriesBudget = minSeriesBudget
	for index := range profile.Dimensions {
		if profile.Dimensions[index].Key == RequestModel {
			for model := range 20 {
				profile.Dimensions[index].Values = append(
					profile.Dimensions[index].Values,
					fmt.Sprintf("model-%d", model),
				)
			}
		}
	}
	tracker, provider, metrics, _ := newTestTrackerWithMetrics(t, WithProfile(profile))
	statuses := []Outcome{OperationStatusOK, OperationStatusError, OperationStatusUnset}
	// Act.
	for model := range 20 {
		for _, status := range statuses {
			require.NoError(t, tracker.Runtime().RecordOperation(context.Background(),
				Operation{Name: "chat", Model: fmt.Sprintf("model-%d", model)},
				OperationResult{Status: status, Duration: time.Millisecond}))
		}
	}
	flushTestProvider(t, provider)
	// Assert: the bounded exporter preserves counts and outcome on overflow series.
	histogram := metrytest.FindFloat64Histogram(
		t,
		metrytest.CollectResourceMetrics(t, provider, metrics),
		OperationDurationMetricName,
	)
	require.LessOrEqual(t, len(histogram.DataPoints), minSeriesBudget)
	counts := map[string]uint64{}
	for _, point := range histogram.DataPoints {
		status := testutil.SpanStringAttr(t, point.Attributes, OperationStatus)
		counts[status] += point.Count
	}
	for _, status := range statuses {
		assert.Equal(t, uint64(20), counts[string(status)])
	}
	require.Len(t, counts, 3)
}
