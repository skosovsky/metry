package metry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	"github.com/skosovsky/metry/metrytest"
)

func TestNew_NoMetricExporter_TrackerWorksViaPublicAPI(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("test-no-metrics-tracker"))

	tracker, err := genai.NewTrackerFromProvider(provider)
	require.NoError(t, err)

	err = tracker.RecordInteraction(ctx, genai.Meta{
		Provider:  "openai",
		Operation: "chat",
	}, genai.Payload{}, genai.Usage{
		InputTokens:  1,
		OutputTokens: 1,
	})
	require.NoError(t, err)

	require.NoError(t, provider.ForceFlush(ctx))
	require.NotEmpty(t, mem.GetSpans())

	require.NoError(t, provider.Shutdown(ctx))
}

func TestNew_WithHintSampler_RequiresKeepHintToExport(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t,
		metry.WithServiceName("test-hint-sampler"),
		metry.WithSampler(genai.NewHintSampler(metry.NeverSample())),
	)

	ctx, end, err := provider.StartSpan(ctx, "metry-test", "span-without-hint")
	require.NoError(t, err)
	end()

	ctx, endKeep, err := provider.StartSpan(ctx, "metry-test", "span-with-hint",
		metry.WithSpanAttributes(metry.BoolAttribute(genai.SamplingKeep, true)),
	)
	require.NoError(t, err)
	endKeep()

	require.NoError(t, provider.ForceFlush(ctx))
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "span-with-hint", spans[0].Name)
}

func TestNew_WithHintSampler_PropagatesSampledParentDecision(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t,
		metry.WithServiceName("test-hint-parent"),
		metry.WithSampler(genai.NewHintSampler(metry.NeverSample())),
	)

	rootCtx, endRoot, err := provider.StartSpan(ctx, "metry-test", "root-with-hint",
		metry.WithSpanAttributes(metry.BoolAttribute(genai.SamplingKeep, true)),
	)
	require.NoError(t, err)
	_, endChild, err := provider.StartSpan(rootCtx, "metry-test", "child-without-hint")
	require.NoError(t, err)
	endChild()
	endRoot()

	require.NoError(t, provider.ForceFlush(ctx))
	spans := mem.GetSpans()
	require.Len(t, spans, 2)
}
