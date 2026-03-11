package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/skosovsky/metry/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPHandler_WrapsHandler(t *testing.T) {
	mem := testutil.SetupTestTracing(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h := Handler(next, "test-op")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
	assert.GreaterOrEqual(t, mem.Len(), 1, "otelhttp should create at least 1 span")
}

func TestHTTPHandler_WithoutInit_DoesNotPanic(t *testing.T) {
	// Ensure handler works even if global tracer is noop (no metry.Init)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := Handler(next, "op")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	require.NotPanics(t, func() {
		h.ServeHTTP(rec, req)
	})
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
