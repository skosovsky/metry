package genaimetrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestRegisterMetrics_NilMeter_ReturnsError(t *testing.T) {
	cleanup, err := RegisterMetrics(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "meter must not be nil")
	require.Nil(t, cleanup, "cleanup must be nil on error")
}

func TestRegisterMetrics_SecondCallWithoutShutdown_ReturnsError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-double-register")

	cleanup1, err1 := RegisterMetrics(meter)
	require.NoError(t, err1)
	t.Cleanup(cleanup1)

	_, err2 := RegisterMetrics(meter)
	require.Error(t, err2)
	assert.ErrorIs(t, err2, ErrMetricsAlreadyRegistered)
}

func TestRegisterMetrics_ConcurrentCalls(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-concurrent")

	const numGoroutines = 10
	type result struct {
		cleanup func()
		err     error
	}
	resCh := make(chan result, numGoroutines)
	for range numGoroutines {
		go func() {
			c, e := RegisterMetrics(meter)
			resCh <- result{c, e}
		}()
	}
	var oneCleanup func()
	successCount := 0
	for range numGoroutines {
		res := <-resCh
		if res.err == nil {
			successCount++
			oneCleanup = res.cleanup
		} else {
			require.ErrorIs(t, res.err, ErrMetricsAlreadyRegistered)
		}
	}
	require.Equal(t, 1, successCount, "exactly one concurrent RegisterMetrics should succeed")
	t.Cleanup(oneCleanup)

	holder := Holder()
	require.NotNil(t, holder, "Holder should be set")
	assert.NotNil(t, holder.InputTokens)
	assert.NotNil(t, holder.OutputTokens)
	assert.NotNil(t, holder.Cost)
	assert.NotNil(t, holder.Ttft)
}

func TestRegisterMetrics_PartialInitializationDoesNotCorruptGlobals(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-partial")

	cleanup, err := RegisterMetrics(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	holder := Holder()
	require.NotNil(t, holder)
	assert.NotNil(t, holder.InputTokens)
	assert.NotNil(t, holder.OutputTokens)

	_, err = RegisterMetrics(nil)
	require.Error(t, err)

	holder = Holder()
	require.NotNil(t, holder)
	assert.NotNil(t, holder.InputTokens)
	assert.NotNil(t, holder.OutputTokens)
}
