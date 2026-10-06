package genai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
)

func TestRuntimeConstructorReturnsConfigurationErrors(t *testing.T) {
	// Arrange.
	provider, _ := metrytest.NewTestProvider(t)
	unsupported := DefaultProfile()
	unsupported.ID = "unsupported"
	mismatch := DefaultProfile()
	mismatch.PayloadPolicyID = profileHashPolicy
	cases := map[string][]Option{
		"zero payload budget":   {WithMaxContextLength(0)},
		"negative event budget": {WithMaxEventLength(-1)},
		"unsupported profile":   {WithProfile(unsupported)},
		"policy mismatch":       {WithProfile(mismatch)},
		"undeclared raw":        {WithPayloadPolicy(RawPayloadPolicy())},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			// Act.
			runtime, err := NewRuntimeFromProvider(provider, opts...)
			// Assert: caller receives error, no usable recorder silently replaces it.
			require.ErrorIs(t, err, ErrInvalidObservation)
			require.Nil(t, runtime)
		})
	}
	// Act.
	runtime, err := NewRuntimeFromProvider(nil)
	// Assert.
	require.ErrorIs(t, err, ErrProviderRequired)
	require.Nil(t, runtime)
}

type failingInstrumentMeter struct {
	metric.Meter

	name string
	err  error
}

func (m failingInstrumentMeter) Int64Histogram(
	name string, opts ...metric.Int64HistogramOption,
) (metric.Int64Histogram, error) {
	if name == m.name {
		return nil, m.err
	}
	return m.Meter.Int64Histogram(name, opts...)
}

func (m failingInstrumentMeter) Float64Histogram(
	name string, opts ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if name == m.name {
		return nil, m.err
	}
	return m.Meter.Float64Histogram(name, opts...)
}

func (m failingInstrumentMeter) Float64Counter(
	name string, opts ...metric.Float64CounterOption,
) (metric.Float64Counter, error) {
	if name == m.name {
		return nil, m.err
	}
	return m.Meter.Float64Counter(name, opts...)
}

type failingInstrumentProvider struct {
	metric.MeterProvider

	meter metric.Meter
}

func (p failingInstrumentProvider) Meter(string, ...metric.MeterOption) metric.Meter { return p.meter }

func TestRuntimeConstructorReturnsInstrumentErrors(t *testing.T) {
	for _, name := range []string{
		InputTokenUsageMetricName, OutputTokenUsageMetricName, ObservedOperationDurationMetricName,
		InvocationDurationMetricName, TokenComponentUsageMetricName, OperationDurationMetricName,
		CostMetricName, TTFTMetricName, DerivedOutputTokensPerSecondMetricName,
		DerivedMeanOutputTokenIntervalMetricName, VideoSecondsMetricName, VideoFramesMetricName,
		ToolDurationMetricName,
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange: fail exactly one registration, including late registrations.
			failure := errors.New("instrument unavailable")
			mp := metricnoop.NewMeterProvider()
			provider, err := metry.FromSDK(metry.SDKDependencies{
				MeterProvider: failingInstrumentProvider{MeterProvider: mp,
					meter: failingInstrumentMeter{Meter: mp.Meter("test"), name: name, err: failure}},
				TracerProvider: tracenoop.NewTracerProvider(), Propagator: propagation.TraceContext{},
			})
			require.NoError(t, err)
			// Act.
			runtime, err := NewRuntimeFromProvider(provider)
			// Assert.
			require.ErrorIs(t, err, failure)
			require.Nil(t, runtime)
		})
	}
}

func TestRuntimeExplicitOptionalComposition(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			// Arrange: the host decides enablement before constructor invocation.
			provider, exports := metrytest.NewTestProvider(t)
			runtime := NoopRuntime()
			if enabled {
				var err error
				runtime, err = NewRuntimeFromProvider(provider)
				require.NoError(t, err)
			}
			// Act.
			require.NoError(t, runtime.RecordOperation(context.Background(),
				Operation{Name: "chat", Locality: ExecutionRemote}, OperationResult{Status: OperationStatusOK}))
			require.NoError(t, provider.ForceFlush(context.Background()))
			// Assert.
			count := 0
			if enabled {
				count = 1
			}
			require.Len(t, exports.GetSpans(), count)
		})
	}
}
