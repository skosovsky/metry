package genai

// GenAI attribute names emitted by metry.
const (
	ProviderName          = "gen_ai.provider.name"
	OperationName         = "gen_ai.operation.name"
	RequestModel          = "gen_ai.request.model"
	ResponseModel         = "gen_ai.response.model"
	SystemInstructions    = "gen_ai.system_instructions"
	InputMessages         = "gen_ai.input.messages"
	OutputMessages        = "gen_ai.output.messages"
	InputTokens           = "gen_ai.usage.input_tokens"             // #nosec G101 -- OTel semantic convention name
	OutputTokens          = "gen_ai.usage.output_tokens"            // #nosec G101 -- OTel semantic convention name
	CacheWriteInputTokens = "gen_ai.usage.cache_write.input_tokens" // #nosec G101 -- OTel semantic convention name
	CacheReadInputTokens  = "gen_ai.usage.cache_read.input_tokens"  // #nosec G101 -- OTel semantic convention name
	TokenType             = "metry.gen_ai.token.component"          // #nosec G101 -- OTel semantic convention name
	ToolName              = "gen_ai.tool.name"
	ToolCallID            = "gen_ai.tool.call.id"
	ToolCallArguments     = "metry.gen_ai.tool.call.arguments"
	ToolCallResult        = "metry.gen_ai.tool.call.result"
	ToolError             = "metry.gen_ai.tool.error"
	EvaluationScore       = "gen_ai.evaluation.score.value"
	EvaluationExplanation = "gen_ai.evaluation.explanation"
	EvaluationMetricName  = "gen_ai.evaluation.name"
	EvaluationLabel       = "gen_ai.evaluation.score.label"
	ServerAddress         = "server.address"
	ServerPort            = "server.port"
	ErrorType             = "error.type"

	UsageCost                  = "metry.gen_ai.usage.cost"
	CostCurrency               = "metry.gen_ai.cost.currency"
	UsageReasoningOutputTokens = "gen_ai.usage.reasoning.output_tokens" // #nosec G101 -- custom semantic convention name
	UsageVideoSeconds          = "metry.gen_ai.usage.video.seconds"
	UsageVideoFrames           = "metry.gen_ai.usage.video.frames"
	AudioSeconds               = "metry.gen_ai.usage.audio_seconds"
	ImageCount                 = "metry.gen_ai.usage.image_count"
	OperationPurpose           = "metry.gen_ai.operation.purpose"
	OperationStatus            = "metry.gen_ai.operation.status"

	RetrievalSource            = "gen_ai.data_source.id"
	RetrievalQuery             = "gen_ai.retrieval.query.text"
	RetrievalTopK              = "gen_ai.retrieval.top_k"
	RetrievalReturnedDocuments = "metry.gen_ai.retrieval.returned_documents"
	SamplingKeep               = "metry.gen_ai.sampling.keep"
	CacheHit                   = "metry.gen_ai.cache.hit"
	EmbeddingModel             = "metry.gen_ai.embedding.model"

	AgentName = "gen_ai.agent.name"
)

// GenAI metric names emitted by metry.
const (
	InputTokenUsageMetricName                = "gen_ai.client.inference.operation.input_tokens" // #nosec G101 -- OTel metric name
	TokenComponentUsageMetricName            = "metry.gen_ai.client.token.component.usage"      // #nosec G101 -- custom metric name
	OperationDurationMetricName              = "gen_ai.client.operation.duration"
	CostMetricName                           = "metry.gen_ai.cost"
	TTFTMetricName                           = "metry.gen_ai.client.ttft"
	DerivedOutputTokensPerSecondMetricName   = "metry.gen_ai.client.derived.output_tokens_per_second"   // #nosec G101 -- Semantic instrument name or formula identifier, not a credential.
	DerivedMeanOutputTokenIntervalMetricName = "metry.gen_ai.client.derived.mean_output_token_interval" // #nosec G101 -- Semantic instrument name or formula identifier, not a credential.
	VideoSecondsMetricName                   = "metry.gen_ai.client.video.seconds"
	VideoFramesMetricName                    = "metry.gen_ai.client.video.frames"
	ToolDurationMetricName                   = "gen_ai.execute_tool.duration"
)

// Well-known and custom token-type values for the token usage histogram.
const (
	TokenComponentCacheWrite = "input_cache_write" // #nosec G101 -- custom token type label
	TokenComponentCacheRead  = "input_cache_read"  // #nosec G101 -- custom token type label
	TokenComponentReasoning  = "output_reasoning"  // #nosec G101 -- custom token type label
)

// Standard metry values for custom operation purpose.
const (
	PurposeGeneration        = "generation"
	PurposeGuardEvaluation   = "guard_evaluation"
	PurposeQualityEvaluation = "quality_evaluation"
)

// Versioned supported semantic mapping and metry extensions.
const (
	OutputTokenUsageMetricName          = "gen_ai.client.inference.operation.output_tokens" // #nosec G101 -- Semantic instrument name or formula identifier, not a credential.
	ObservedOperationDurationMetricName = "metry.gen_ai.operation.duration"
	InvocationDurationMetricName        = "metry.gen_ai.invocation.duration"
	InvocationKindKey                   = "metry.gen_ai.invocation.kind"
	ExecutionLocalityKey                = "metry.gen_ai.execution.locality"
	AgentID                             = "gen_ai.agent.id"
	AgentVersion                        = "gen_ai.agent.version"
	ConversationID                      = "gen_ai.conversation.id"
	LocalAgentID                        = "metry.gen_ai.agent.id"
	WorkflowName                        = "gen_ai.workflow.name"
	WorkflowID                          = "metry.gen_ai.workflow.id"
	WorkflowVersion                     = "metry.gen_ai.workflow.version"
	RetrievalName                       = "metry.gen_ai.retrieval.name"
	RetrievalDocuments                  = "gen_ai.retrieval.documents"
	RetrievalDocumentsTruncated         = "metry.gen_ai.retrieval.documents.truncated"
)

// Derived stream calculations describe aggregate estimates, not chunk measurements.
const (
	DerivedCalculationKey               = "metry.gen_ai.stream.calculation"
	DerivedOutputTokenRateCalculation   = "output_tokens_over_generation_window" // #nosec G101 -- Semantic instrument name or formula identifier, not a credential.
	DerivedMeanTokenIntervalCalculation = "generation_window_over_output_token_intervals"
)

const payloadToolRole = "tool"
