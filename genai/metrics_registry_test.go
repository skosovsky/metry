package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestRegisterMetricsForInit_NilMeter_ReturnsError(t *testing.T) {
	cleanup, err := RegisterMetricsForInit(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "meter must not be nil")
	require.Nil(t, cleanup, "cleanup must be nil on error")
}

func TestRegisterMetricsForInit_SecondCallWithoutShutdown_ReturnsError(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-double-register")

	cleanup1, err1 := RegisterMetricsForInit(meter)
	require.NoError(t, err1)
	t.Cleanup(cleanup1)

	_, err2 := RegisterMetricsForInit(meter)
	require.Error(t, err2)
	assert.ErrorIs(t, err2, errMetricsAlreadyRegistered)
}

func TestRegisterMetricsForInit_ConcurrentCalls(t *testing.T) {
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
			c, e := RegisterMetricsForInit(meter)
			resCh <- result{cleanup: c, err: e}
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
			require.ErrorIs(t, res.err, errMetricsAlreadyRegistered)
		}
	}
	require.Equal(t, 1, successCount, "exactly one concurrent RegisterMetricsForInit should succeed")
	t.Cleanup(oneCleanup)

	holder := currentMetricsHolder()
	require.NotNil(t, holder, "metrics holder should be set")
	assert.NotNil(t, holder.InputTokens)
	assert.NotNil(t, holder.OutputTokens)
	assert.NotNil(t, holder.Cost)
	assert.NotNil(t, holder.Ttft)
}

func TestRegisterMetricsForInit_PartialInitializationDoesNotCorruptGlobals(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("genai-test-partial")

	cleanup, err := RegisterMetricsForInit(meter)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	holder := currentMetricsHolder()
	require.NotNil(t, holder)
	assert.NotNil(t, holder.InputTokens)
	assert.NotNil(t, holder.OutputTokens)

	_, err = RegisterMetricsForInit(nil)
	require.Error(t, err)

	holder = currentMetricsHolder()
	require.NotNil(t, holder)
	assert.NotNil(t, holder.InputTokens)
	assert.NotNil(t, holder.OutputTokens)
}
