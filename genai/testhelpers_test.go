package genai

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
	"github.com/skosovsky/metry/testutil"
)

func newTestTracker(t *testing.T, opts ...Option) (*Tracker, *metry.Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("genai-test"))
	opts = append([]Option{WithProfile(testInstrumentationProfile())}, opts...)
	tracker, err := NewTrackerFromProvider(provider, opts...)
	require.NoError(t, err)
	return tracker, provider, mem
}

func newTestTrackerWithMetrics(
	t *testing.T,
	opts ...Option,
) (*Tracker, *metry.Provider, *testutil.InMemoryMetricExporter, *testutil.InMemoryTraceExporter) {
	t.Helper()
	memMetric := testutil.NewInMemoryMetricExporter()
	provider, memTrace := metrytest.NewTestProvider(t,
		metry.WithServiceName("genai-test"),
		metry.WithMetricExporter(memMetric.SDKExporter()),
	)
	opts = append([]Option{WithProfile(testInstrumentationProfile())}, opts...)
	tracker, err := NewTrackerFromProvider(provider, opts...)
	require.NoError(t, err)
	return tracker, provider, memMetric, memTrace
}

func newTestTrackerWithSampler(
	t *testing.T,
	sampler sdktrace.Sampler,
) (*Tracker, *metry.Provider, *testutil.InMemoryTraceExporter) {
	t.Helper()
	provider, mem := metrytest.NewTestProvider(t,
		metry.WithServiceName("genai-test"),
		metry.WithSampler(sampler),
	)
	tracker, err := NewTrackerFromProvider(provider, WithProfile(testInstrumentationProfile()))
	require.NoError(t, err)
	return tracker, provider, mem
}

func flushTestProvider(t *testing.T, provider *metry.Provider) {
	t.Helper()
	require.NoError(t, provider.ForceFlush(context.Background()))
}

func testInstrumentationProfile() Profile {
	profile := DefaultProfile()
	for index := range profile.Dimensions {
		switch profile.Dimensions[index].Key {
		case CostCurrency:
			profile.Dimensions[index].Values = append(profile.Dimensions[index].Values, "CREDITS")
		case ProviderName:
			profile.Dimensions[index].Values = []string{
				"scope-provider",
				"gemini",
				"openai",
				"scope-openai",
				"explicit",
				"qdrant",
			}
		case RequestModel, ResponseModel:
			profile.Dimensions[index].Values = []string{"model", "gpt-4o-mini", "scope-model"}
		case OperationName:
			profile.Dimensions[index].Values = append(profile.Dimensions[index].Values,
				"scope-chat", "enqueue_summary", "summary_done", "delivery.success", "vector.search")
		case ToolName:
			profile.Dimensions[index].Values = []string{"search"}
		case ErrorType:
			profile.Dimensions[index].Values = append(profile.Dimensions[index].Values, "provider_overloaded")
		}
	}
	return profile
}
