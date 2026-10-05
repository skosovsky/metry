package genai

import (
	"context"
	"math"
	"unicode/utf8"

	"github.com/skosovsky/metry"
)

// RecordAsyncFeedback records delayed user feedback as a new span linked to the original interaction.
func (t *Tracker) RecordAsyncFeedback(
	ctx context.Context,
	handle metry.AsyncHandle,
	score float64,
	feedbackText string,
	opts ...LinkedSpanOption,
) error {
	if !handle.IsValid() {
		return ErrInvalidAsyncHandle
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
					truncateContextWithLimit(feedbackText, t.cfg.MaxEventLength()),
				),
			)
		}
		return nil
	}, spanOpts...)
}
