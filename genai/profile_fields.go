package genai

// completeProfileFields is the canonical catalogue of metry-owned attributes.
// Host SDK/custom attributes are explicitly outside this checked declaration.
func completeProfileFields(fields []ProfileField) []ProfileField {
	for _, group := range []struct {
		encoding  string
		stability Stability
		names     []string
	}{
		{EncodingString, StabilityStable, []string{"exception.type", "exception.message", "exception.stacktrace"}},
		{EncodingString, StabilityDevelopment, []string{ToolName, ToolCallID, EvaluationMetricName, EvaluationText, EvaluationReasoning}},
		{EncodingJSON, StabilityDevelopment, []string{ToolCallArguments, ToolCallResult}},
		{EncodingString, StabilityExtension, []string{
			CostCurrency, OperationPurpose, OperationStatus, RetrievalSource, RetrievalProvider, RetrievalQuery,
			EmbeddingModel, AgentName, AgentRole, WorkflowStep,
			causalRunKey, causalParentRunKey, causalOperationKey, causalStepKey, causalAttemptKey, causalKindKey, causalPromptKey,
			"metry.causal.predecessor", "metry.observation.kind", "metry.usage.price_reference", "metry.usage.source",
			evaluationStatusKey, evaluationObservationKey, evaluationOutcomeKey,
			evaluationExperimentKey, evaluationDatasetKey, evaluationCaseKey,
			evaluationTrialKey, evaluationTargetKey, evaluationGraderKey,
			evaluationArtifactKey, "metry.diagnostic.field", "metry.diagnostic.rule",
			TokenType, ToolMetricLabelTool, ToolMetricLabelStatus, ToolMetricLabelErrorType,
		}},
		{EncodingBoolean, StabilityExtension, []string{ToolError, CacheHit, SamplingKeep, overflowKey}},
		{EncodingInteger, StabilityExtension, []string{RetrievalTopK, RetrievalReturnedChunks}},
		{EncodingFloat, StabilityDevelopment, []string{EvaluationScore}},
		{EncodingFloatArray, StabilityExtension, []string{RetrievalDistances}},
	} {
		for _, name := range group.names {
			fields = append(
				fields,
				ProfileField{
					Name:      name,
					Unit:      "",
					Stability: group.stability,
					Required:  false,
					Encoding:  group.encoding,
				},
			)
		}
	}
	for _, measurement := range []struct {
		name     string
		unit     Unit
		encoding string
	}{
		{AudioSeconds, UnitSeconds, EncodingFloat},
		{ImageCount, UnitImages, EncodingInteger},
		{UsageVideoSeconds, UnitSeconds, EncodingFloat},
		{UsageVideoFrames, UnitFrames, EncodingInteger},
	} {
		fields = append(
			fields,
			ProfileField{
				Name:      measurement.name,
				Unit:      measurement.unit,
				Stability: StabilityExtension,
				Required:  false,
				Encoding:  measurement.encoding,
			},
		)
	}
	for _, name := range []string{InputTokens, OutputTokens, CacheReadInputTokens, CacheCreationInputTokens,
		UsageReasoningOutputTokens, UsageCost, AudioSeconds, ImageCount, UsageVideoSeconds, UsageVideoFrames} {
		for _, suffix := range []string{".source", ".unit"} {
			fields = append(
				fields,
				ProfileField{
					Name:      name + suffix,
					Unit:      "",
					Stability: StabilityExtension,
					Required:  false,
					Encoding:  EncodingString,
				},
			)
		}
	}
	return fields
}
