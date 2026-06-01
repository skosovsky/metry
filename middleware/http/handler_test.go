// revive:disable-next-line var-naming -- package name "http" is intentional for HTTP middleware
package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/testutil"
)

func TestHTTPHandler_WrapsHandler(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewInMemoryTraceExporter()

	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-http"),
		metry.WithExporter(mem.SpanExporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h := Handler(provider, next, "test-op")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	tp, ok := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.True(t, ok)
	require.NoError(t, tp.ForceFlush(ctx))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
	assert.GreaterOrEqual(t, mem.Len(), 1, "otelhttp should create at least 1 span")
}

func TestHTTPHandler_NilProvider_Panics(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	require.Panics(t, func() {
		_ = Handler(nil, next, "op")
	})
}

func TestHTTPHandler_WithSpanNameFormatter(t *testing.T) {
	ctx := context.Background()
	mem := testutil.NewInMemoryTraceExporter()

	provider, err := metry.New(
		ctx,
		metry.WithServiceName("test-http"),
		metry.WithExporter(mem.SpanExporter()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = provider.Shutdown(ctx) })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /named", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := Handler(provider, mux, "ignored",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if r.Pattern != "" {
				return r.Pattern
			}
			return "fallback"
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/named", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	tp, ok := provider.TracerProvider.(*sdktrace.TracerProvider)
	require.True(t, ok)
	require.NoError(t, tp.ForceFlush(ctx))
	require.GreaterOrEqual(t, mem.Len(), 1)
	spans := mem.GetSpans()
	require.NotEmpty(t, spans)
	assert.Equal(t, "GET /named", spans[len(spans)-1].Name)
}
