package genai

import (
	"context"
	"math"
	"unicode/utf8"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/internal/genaiwire"
)

// RecordAsyncFeedback records delayed user feedback as a new span linked to the original interaction.
func (t *Tracker) RecordAsyncFeedback(
	ctx context.Context,
	handle metry.AsyncHandle,
	score float64,
	feedbackText string,
	opts ...LinkedSpanOption,
) error {
	spanContext, err := genaiwire.AsyncSpanContext(handle)
	if err != nil {
		return ErrInvalidAsyncHandle
	}
	if !spanContext.IsValid() {
		return nil
	}
	if math.IsNaN(score) || math.IsInf(score, 0) || len(feedbackText) > maxPayloadInputBytes ||
		!utf8.ValidString(feedbackText) {
		return invalidObservation("feedback", "invalid_or_out_of_bounds")
	}

	spanOpts := append([]LinkedSpanOption{
		metry.WithLinkedAttributes(metry.FloatAttribute(EvaluationScore, score)),
	}, opts...)

	return handle.RecordLinkedSpan(ctx, t.provider, "user_feedback", func(w metry.LinkedSpanWriter) error {
		if t.cfg.RecordPayloads() && feedbackText != "" {
			feedbackText = sanitizeTextWithConfig(feedbackText, t.cfg)
			w.SetAttributes(
				metry.StringAttribute(
					EvaluationExplanation,
					truncateContextWithLimit(feedbackText, t.cfg.EventTextByteLimit()),
				),
			)
		}
		return nil
	}, spanOpts...)
}
