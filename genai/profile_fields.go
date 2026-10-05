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
		{EncodingString, StabilityDevelopment, []string{ToolName, ToolCallID, EvaluationMetricName, EvaluationExplanation, EvaluationLabel, RetrievalSource, RetrievalQuery, AgentName, AgentID, AgentVersion, ConversationID, WorkflowName}},
		{EncodingJSON, StabilityDevelopment, []string{RetrievalDocuments}},
		{EncodingJSON, StabilityExtension, []string{ToolCallArguments, ToolCallResult}},
		{EncodingString, StabilityExtension, []string{
			CostCurrency, OperationPurpose, OperationStatus, EmbeddingModel,
			RetrievalName, LocalAgentID, WorkflowID, WorkflowVersion, InvocationKindKey, ExecutionLocalityKey, DerivedCalculationKey,
			causalRunKey, causalParentRunKey, causalOperationKey, causalStepKey, causalAttemptKey, causalKindKey, causalPromptKey,
			"metry.causal.predecessor", "metry.observation.kind", "metry.usage.price_reference", "metry.usage.source", "metry.usage.mapping",
			evaluationStatusKey, evaluationObservationKey,
			evaluationExperimentKey, evaluationDatasetKey, evaluationCaseKey, evaluationTrialKey, evaluationTargetKey, evaluationGraderKey, evaluationArtifactKey,
			"metry.diagnostic.field", "metry.diagnostic.rule", TokenType,
		}},
		{EncodingBoolean, StabilityExtension, []string{ToolError, CacheHit, SamplingKeep, overflowKey, RetrievalDocumentsTruncated,
			"metry.gen_ai.payload.system_instructions.truncated", "metry.gen_ai.payload.input_messages.truncated", "metry.gen_ai.payload.output_messages.truncated"}},
		{EncodingInteger, StabilityDevelopment, []string{RetrievalTopK}},
		{EncodingInteger, StabilityExtension, []string{RetrievalReturnedDocuments}},
		{EncodingFloat, StabilityDevelopment, []string{EvaluationScore}},
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
	for _, name := range []string{InputTokens, OutputTokens, CacheReadInputTokens, CacheWriteInputTokens,
		UsageReasoningOutputTokens, UsageCost, AudioSeconds, ImageCount, UsageVideoSeconds, UsageVideoFrames} {
		for _, suffix := range []string{".source", ".unit"} {
			fields = append(
				fields,
				ProfileField{
					Name:      measurementProvenanceKey(name) + suffix,
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
