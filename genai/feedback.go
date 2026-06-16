package genai

import (
	"context"

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

	spanOpts := append([]LinkedSpanOption{
		metry.WithLinkedAttributes(metry.FloatAttribute(EvaluationScore, score)),
	}, opts...)

	return handle.RecordLinkedSpan(ctx, t.provider, "user_feedback", func(w metry.LinkedSpanWriter) error {
		if t.cfg.RecordPayloads() && feedbackText != "" {
			w.SetAttributes(metry.StringAttribute(EvaluationText, truncateContextWithConfig(feedbackText, t.cfg)))
		}
		return nil
	}, spanOpts...)
}
