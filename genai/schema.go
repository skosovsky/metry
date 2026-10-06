package genai

// SchemaDescriptor describes the single supported observation schema. It has no
// writable state; all collection methods return detached declarations.
type SchemaDescriptor struct{}

// Schema returns the fixed schema independently of runtime configuration.
func Schema() SchemaDescriptor { return SchemaDescriptor{} }

// ID identifies the observation schema.
func (SchemaDescriptor) ID() string { return InstrumentationProfileID }

// Mapping identifies the fixed inclusive token accounting mapping.
func (SchemaDescriptor) Mapping() string { return profileInclusiveMapping }

// Fields returns the complete metry-owned field catalogue.
//
//nolint:exhaustruct_v5 // Optional catalogue properties intentionally remain zero.
func (SchemaDescriptor) Fields() []ProfileField {
	return completeProfileFields([]ProfileField{
		{Name: ProviderName, Stability: StabilityDevelopment, Encoding: EncodingString},
		{Name: OperationName, Stability: StabilityDevelopment, Encoding: EncodingString, Required: true},
		{Name: RequestModel, Stability: StabilityDevelopment, Encoding: EncodingString},
		{Name: ResponseModel, Stability: StabilityDevelopment, Encoding: EncodingString},
		{Name: ServerAddress, Stability: StabilityStable, Encoding: EncodingString},
		{Name: ServerPort, Stability: StabilityStable, Encoding: EncodingInteger},
		{Name: ErrorType, Stability: StabilityStable, Encoding: EncodingString},
		{Name: InputTokens, Unit: UnitTokens, Stability: StabilityDevelopment, Encoding: EncodingInteger},
		{Name: OutputTokens, Unit: UnitTokens, Stability: StabilityDevelopment, Encoding: EncodingInteger},
		{Name: CacheReadInputTokens, Unit: UnitTokens, Stability: StabilityDevelopment, Encoding: EncodingInteger},
		{
			Name:      CacheWriteInputTokens,
			Unit:      UnitTokens,
			Stability: StabilityDevelopment,
			Encoding:  EncodingInteger,
		},
		{
			Name:      UsageReasoningOutputTokens,
			Unit:      UnitTokens,
			Stability: StabilityDevelopment,
			Encoding:  EncodingInteger,
		},
		{Name: UsageCost, Unit: UnitCurrency, Stability: StabilityExtension, Encoding: EncodingFloat},
		{Name: InputMessages, Stability: StabilityDevelopment, Encoding: EncodingJSON},
		{Name: OutputMessages, Stability: StabilityDevelopment, Encoding: EncodingJSON},
		{Name: SystemInstructions, Stability: StabilityDevelopment, Encoding: EncodingJSON},
	})
}

// Boundaries returns the fixed instrumentation ownership boundaries.
func (SchemaDescriptor) Boundaries() []Boundary {
	return []Boundary{
		{Name: profileModelBoundary, Operation: profileChatOperation, Accounting: true},
		{Name: payloadToolRole, Operation: profileToolOperation, Accounting: true},
		{Name: profileRetrievalOperation, Operation: profileRetrievalOperation, Accounting: false},
		{Name: string(IdentityLogical), Operation: string(IdentityLogical), Accounting: false},
		{Name: profileEvaluationOperation, Operation: profileEvaluationOperation, Accounting: false},
		{Name: "agent", Operation: "invoke_agent", Accounting: false},
		{Name: "workflow", Operation: "invoke_workflow", Accounting: false},
	}
}
