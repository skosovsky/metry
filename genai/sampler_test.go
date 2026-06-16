package genai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
)

func TestNewHintSampler_Integration_KeepHintExportsSpan(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t,
		metry.WithServiceName("genai-sampler-test"),
		metry.WithSampler(NewHintSampler(metry.NeverSample())),
	)

	_, end, err := provider.StartSpan(ctx, "metry-test", "span-without-hint")
	require.NoError(t, err)
	end()

	_, endKeep, err := provider.StartSpan(ctx, "metry-test", "span-with-hint",
		metry.WithSpanAttributes(metry.BoolAttribute(SamplingKeep, true)),
	)
	require.NoError(t, err)
	endKeep()

	require.NoError(t, provider.ForceFlush(ctx))
	spans := mem.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "span-with-hint", spans[0].Name)
}

func TestNewHintSampler_Integration_PropagatesSampledParent(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t,
		metry.WithServiceName("genai-sampler-test"),
		metry.WithSampler(NewHintSampler(metry.NeverSample())),
	)

	rootCtx, endRoot, err := provider.StartSpan(ctx, "metry-test", "root-with-hint",
		metry.WithSpanAttributes(metry.BoolAttribute(SamplingKeep, true)),
	)
	require.NoError(t, err)
	_, endChild, err := provider.StartSpan(rootCtx, "metry-test", "child-without-hint")
	require.NoError(t, err)
	endChild()
	endRoot()

	require.NoError(t, provider.ForceFlush(ctx))
	require.Len(t, mem.GetSpans(), 2)
}
