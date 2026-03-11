package metry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestOTLPGRPC_ReturnsNonNilExporters(t *testing.T) {
	te, me := OTLPGRPC("localhost:4317", true)
	require.NotNil(t, te)
	require.NotNil(t, me)
	assert.NotNil(t, te.create)
	assert.NotNil(t, me.create)
}

func TestOTLPHTTP_ReturnsNonNilExporters(t *testing.T) {
	te, me := OTLPHTTP("localhost:4318", map[string]string{"X-Custom": "value"})
	require.NotNil(t, te)
	require.NotNil(t, me)
	assert.NotNil(t, te.create)
	assert.NotNil(t, me.create)
}

func TestOTLPHTTP_EmptyHeaders(t *testing.T) {
	te, me := OTLPHTTP("localhost:4318", nil)
	require.NotNil(t, te)
	require.NotNil(t, me)
}

func TestConsole_ReturnsNonNilExporters(t *testing.T) {
	te, me := Console()
	require.NotNil(t, te)
	require.NotNil(t, me)
	ctx := context.Background()
	res := resource.NewWithAttributes("", attribute.String("service.name", "test"))
	spanExp, err := te.create(ctx, res)
	require.NoError(t, err)
	require.NotNil(t, spanExp)
	metricExp, err := me.create(ctx, res)
	require.NoError(t, err)
	require.NotNil(t, metricExp)
}

func TestNoop_ReturnsWorkingExporters(t *testing.T) {
	te, me := Noop()
	require.NotNil(t, te)
	require.NotNil(t, me)
	ctx := context.Background()
	res := resource.NewWithAttributes("", attribute.String("service.name", "test"))
	spanExp, err := te.create(ctx, res)
	require.NoError(t, err)
	require.NotNil(t, spanExp)
	metricExp, err := me.create(ctx, res)
	require.NoError(t, err)
	require.NotNil(t, metricExp)
}
