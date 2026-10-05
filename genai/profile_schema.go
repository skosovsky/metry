package genai

// EnumDeclaration describes finite wire values. PolicyEncodedUnknown identifies
// structured metadata whose unknown values go through the configured policy.
type EnumDeclaration struct {
	Name                 string
	Values               []string
	PolicyEncodedUnknown bool
}

// Enumerations returns detached schema declarations, never a mutable registry.
func (p Profile) Enumerations() []EnumDeclaration {
	enums := []EnumDeclaration{
		{
			Name:                 causalKindKey,
			Values:               []string{string(IdentityLogical), string(IdentityOperation), string(IdentityAttempt)},
			PolicyEncodedUnknown: false,
		},
		{
			Name: "metry.causal.predecessor",
			Values: []string{
				string(PredecessorUnknown),
				string(PredecessorExported),
				string(PredecessorMissing),
			},
			PolicyEncodedUnknown: false,
		},
		{
			Name:                 OperationStatus,
			Values:               []string{OperationStatusOK, OperationStatusError, OperationStatusUnset},
			PolicyEncodedUnknown: false,
		},
		{
			Name: evaluationStatusKey,
			Values: []string{
				string(EvaluationSucceeded),
				string(EvaluationFailed),
				string(EvaluationSkipped),
			},
			PolicyEncodedUnknown: false,
		},
		{
			Name: "message.role",
			Values: []string{
				"system",
				payloadUserRole,
				payloadAssistantRole,
				ToolMetricLabelTool,
				"developer",
				"function",
			},
			PolicyEncodedUnknown: true,
		},
		{
			Name: "message.finish_reason",
			Values: []string{
				payloadStopFinish,
				"length",
				"tool_calls",
				"content_filter",
				OperationStatusError,
			},
			PolicyEncodedUnknown: true,
		},
		{
			Name: "content.type",
			Values: []string{
				payloadTextType,
				payloadImageType,
				payloadImageURLType,
				payloadAudioType,
				payloadVideoType,
				payloadFileType,
				payloadToolCallType,
				"tool_result",
				reasoningLabel,
			},
			PolicyEncodedUnknown: true,
		},
	}
	for _, dimension := range p.Dimensions {
		if dimension.Key == ErrorType || dimension.Key == ToolMetricLabelErrorType {
			enums = append(enums, EnumDeclaration{
				Name: dimension.Key, Values: append([]string(nil), dimension.Values...), PolicyEncodedUnknown: false,
			})
		}
	}
	enums = append(
		enums,
		EnumDeclaration{
			Name: TokenType,
			Values: []string{
				TokenTypeInput, TokenTypeOutput, TokenTypeInputCacheRead,
				TokenTypeInputCacheCreation, TokenTypeOutputReasoning,
			},
			PolicyEncodedUnknown: false,
		},
		EnumDeclaration{
			Name:                 usageSourceKey,
			Values:               []string{string(SourceMeasured), string(SourceProvider), string(SourceEstimated)},
			PolicyEncodedUnknown: false,
		},
		EnumDeclaration{
			Name:                 ToolMetricLabelStatus,
			Values:               []string{OperationStatusOK, OperationStatusError, OperationStatusUnset},
			PolicyEncodedUnknown: false,
		},
		EnumDeclaration{
			Name: "metry.observation.kind", Values: []string{"completed"}, PolicyEncodedUnknown: false,
		},
	)
	return append(enums, usageEnumerations(p.Fields)...)
}

func usageEnumerations(fields []ProfileField) []EnumDeclaration {
	enums := make([]EnumDeclaration, 0, len(fields))
	for _, field := range fields {
		if field.Unit == "" {
			continue
		}
		enums = append(
			enums,
			EnumDeclaration{
				Name:                 field.Name + ".source",
				Values:               []string{string(SourceMeasured), string(SourceProvider), string(SourceEstimated)},
				PolicyEncodedUnknown: false,
			},
			EnumDeclaration{
				Name:                 field.Name + ".unit",
				Values:               []string{string(field.Unit)},
				PolicyEncodedUnknown: false,
			},
		)
	}
	return enums
}

// EventDeclaration describes library-owned events and their allowed attributes.
// Owner identifies instrumentation ownership, not execution lifecycle ownership.
type EventDeclaration struct {
	Name      string
	Owner     string
	Stability Stability
	Fields    []string
}

// Events returns detached declarations for events emitted by controlled GenAI
// recording paths. Direct host SDK events remain outside this contract.
func (Profile) Events() []EventDeclaration {
	const owner = "metry.genai"
	return []EventDeclaration{
		{Name: AgentStepEvent, Owner: owner, Stability: StabilityExtension,
			Fields: []string{AgentName, AgentRole, WorkflowStep}},
		{Name: "metry.observation.rejected", Owner: owner, Stability: StabilityExtension,
			Fields: []string{"metry.diagnostic.field", "metry.diagnostic.rule"}},
		{Name: "exception", Owner: owner, Stability: StabilityStable,
			Fields: []string{"exception.type", "exception.message", "exception.stacktrace"}},
		{Name: profileEvaluationOperation, Owner: owner, Stability: StabilityExtension,
			Fields: []string{
				OperationName,
				OperationPurpose,
				EvaluationMetricName,
				EvaluationScore,
				EvaluationReasoning,
				evaluationStatusKey,
				evaluationObservationKey,
				evaluationOutcomeKey,
				evaluationExperimentKey,
				evaluationDatasetKey,
				evaluationCaseKey,
				evaluationTrialKey,
				evaluationTargetKey,
				evaluationGraderKey,
				evaluationArtifactKey,
			}},
	}
}

// PayloadChannel declares a controlled input/export boundary. BudgetSetting
// identifies the runtime option that controls output; input limits are fixed.
type PayloadChannel struct {
	Attribute       string
	Encoding        string
	InputByteLimit  int
	InputDepthLimit int
	InputEntryLimit int
	OutputByteLimit int
	BudgetSetting   string
}

// PayloadChannels returns detached channel declarations for the supported schema.
func (Profile) PayloadChannels() []PayloadChannel {
	channels := make([]PayloadChannel, 0, attrCapPayloadAttrs)
	for _, name := range []string{SystemInstructions, InputMessages, OutputMessages, ToolCallArguments, ToolCallResult} {
		channels = append(channels, PayloadChannel{
			Attribute:       name,
			Encoding:        EncodingJSON,
			InputByteLimit:  maxPayloadInputBytes,
			InputDepthLimit: maxPayloadJSONDepth,
			InputEntryLimit: maxPayloadEntries,
			OutputByteLimit: 0,
			BudgetSetting:   "max_context_length",
		})
	}
	for _, name := range []string{RetrievalQuery, EvaluationText, EvaluationReasoning} {
		budget := "max_context_length"
		if name == EvaluationReasoning {
			budget = "max_event_length"
		}
		channels = append(channels, PayloadChannel{
			Attribute:       name,
			Encoding:        EncodingString,
			InputByteLimit:  maxPayloadInputBytes,
			InputDepthLimit: 0,
			InputEntryLimit: 0,
			OutputByteLimit: 0,
			BudgetSetting:   budget,
		})
	}
	return channels
}
