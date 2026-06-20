// Package genai defines GenAI semantic-convention constants and helpers for metry.
package genai

// GenAI attribute names emitted by metry.
const (
	ProviderName             = "gen_ai.provider.name"
	OperationName            = "gen_ai.operation.name"
	RequestModel             = "gen_ai.request.model"
	ResponseModel            = "gen_ai.response.model"
	SystemInstructions       = "gen_ai.system_instructions"
	InputMessages            = "gen_ai.input.messages"
	OutputMessages           = "gen_ai.output.messages"
	InputTokens              = "gen_ai.usage.input_tokens"                // #nosec G101 -- OTel semantic convention name
	OutputTokens             = "gen_ai.usage.output_tokens"               // #nosec G101 -- OTel semantic convention name
	CacheCreationInputTokens = "gen_ai.usage.cache_creation.input_tokens" // #nosec G101 -- OTel semantic convention name
	CacheReadInputTokens     = "gen_ai.usage.cache_read.input_tokens"     // #nosec G101 -- OTel semantic convention name
	TokenType                = "gen_ai.token.type"                        // #nosec G101 -- OTel semantic convention name
	ToolName                 = "gen_ai.tool.name"
	ToolCallID               = "gen_ai.tool.call.id"
	ToolCallArguments        = "gen_ai.tool.call.arguments"
	ToolCallResult           = "gen_ai.tool.call.result"
	ToolError                = "gen_ai.tool.error"
	EvaluationScore          = "gen_ai.evaluation.score"
	EvaluationText           = "gen_ai.evaluation.text"
	EvaluationMetricName     = "gen_ai.evaluation.metric"
	EvaluationReasoning      = "gen_ai.evaluation.reasoning"
	ServerAddress            = "server.address"
	ServerPort               = "server.port"
	ErrorType                = "error.type"

	UsageCost                  = "gen_ai.usage.cost"
	CostCurrency               = "gen_ai.cost.currency"
	UsageReasoningOutputTokens = "gen_ai.usage.reasoning.output_tokens" // #nosec G101 -- custom semantic convention name
	UsageVideoSeconds          = "gen_ai.usage.video.seconds"
	UsageVideoFrames           = "gen_ai.usage.video.frames"
	AudioSeconds               = "gen_ai.usage.audio_seconds"
	ImageCount                 = "gen_ai.usage.image_count"
	OperationPurpose           = "ai.operation.purpose"
	OperationStatus            = "metry.gen_ai.operation.status"

	RetrievalSource         = "gen_ai.retrieval.source"
	RetrievalProvider       = "gen_ai.retrieval.provider"
	RetrievalQuery          = "gen_ai.retrieval.query"
	RetrievalTopK           = "gen_ai.retrieval.top_k"
	RetrievalReturnedChunks = "gen_ai.retrieval.returned_chunks"
	RetrievalDistances      = "gen_ai.retrieval.distances"
	SamplingKeep            = "gen_ai.sampling.keep"
	CacheHit                = "gen_ai.cache.hit"
	EmbeddingModel          = "gen_ai.embedding.model"

	AgentName      = "gen_ai.agent.name"
	AgentRole      = "gen_ai.agent.role"
	WorkflowStep   = "gen_ai.workflow.step"
	AgentStepEvent = "gen_ai.agent.step"
)

// GenAI metric names emitted by metry.
const (
	TokenUsageMetricName          = "gen_ai.client.token.usage"                 // #nosec G101 -- OTel metric name
	TokenComponentUsageMetricName = "metry.gen_ai.client.token.component.usage" // #nosec G101 -- custom metric name
	OperationDurationMetricName   = "gen_ai.client.operation.duration"
	CostMetricName                = "gen_ai.cost"
	TTFTMetricName                = "metry.gen_ai.client.ttft"
	StreamingTPSMetricName        = "metry.gen_ai.client.tps"
	StreamingTBTMetricName        = "metry.gen_ai.client.tbt"
	VideoSecondsMetricName        = "metry.gen_ai.client.video.seconds"
	VideoFramesMetricName         = "metry.gen_ai.client.video.frames"
	ToolDurationMetricName        = "metry.gen_ai.tool.duration"
)

// Tool runtime metric labels emitted by metry.
const (
	ToolMetricLabelTool      = "tool"
	ToolMetricLabelStatus    = "status"
	ToolMetricLabelErrorType = "error_type"
)

// Well-known and custom token-type values for the token usage histogram.
const (
	TokenTypeInput              = "input"                // #nosec G101 -- token type label
	TokenTypeOutput             = "output"               // #nosec G101 -- token type label
	TokenTypeInputCacheCreation = "input_cache_creation" // #nosec G101 -- custom token type label
	TokenTypeInputCacheRead     = "input_cache_read"     // #nosec G101 -- custom token type label
	TokenTypeOutputReasoning    = "output_reasoning"     // #nosec G101 -- custom token type label
)

// Standard metry values for custom operation purpose.
const (
	PurposeGeneration        = "generation"
	PurposeGuardEvaluation   = "guard_evaluation"
	PurposeQualityEvaluation = "quality_evaluation"
)
