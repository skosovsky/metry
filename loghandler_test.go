package metry

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextHandler_WithAttrsAndGroup_Delegate(t *testing.T) {
	var buf bytes.Buffer
	h := ContextHandler{Handler: slog.NewJSONHandler(&buf, nil)}
	grouped := h.WithGroup("svc").WithAttrs([]slog.Attr{slog.String("layer", "api")})
	requireNotNilHandler(t, grouped)
}

func TestContextHandler_Enabled_Delegates(t *testing.T) {
	h := ContextHandler{Handler: slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})}
	assert.False(t, h.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, h.Enabled(context.Background(), slog.LevelError))
}

func requireNotNilHandler(t *testing.T, h slog.Handler) {
	t.Helper()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}
