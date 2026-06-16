package metry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/metrytest"
)

func TestHTTPHandler_CreatesSpan(t *testing.T) {
	ctx := context.Background()
	provider, mem := metrytest.NewTestProvider(t, metry.WithServiceName("root-http"))

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := metry.HTTPHandler(provider, next, "root-op")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.NoError(t, provider.ForceFlush(ctx))
	assert.Equal(t, http.StatusOK, rec.Code)
	require.GreaterOrEqual(t, mem.Len(), 1)
	assert.Equal(t, "root-op", mem.GetSpans()[0].Name)
	assert.NotEqual(t, 2, int(mem.GetSpans()[0].Status.Code)) // otel codes.Error
}

func TestHTTPHandler_NilProvider_Panics(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	require.Panics(t, func() {
		_ = metry.HTTPHandler(nil, next, "op")
	})
}
