package genai

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/metry/internal/traceutil"
)

func mutateSpan(ctx context.Context, fn func(trace.Span)) {
	traceutil.MutateRecordingSpan(trace.SpanFromContext(ctx), fn)
}
