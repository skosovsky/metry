// Package genai defines GenAI semantic-convention constants and helpers for metry.
package genai

import "go.opentelemetry.io/otel/attribute"

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
)

// Attribute keys derived from the string constants above.
var (
	ProviderNameKey               = attribute.Key(ProviderName)
	OperationNameKey              = attribute.Key(OperationName)
	RequestModelKey               = attribute.Key(RequestModel)
	ResponseModelKey              = attribute.Key(ResponseModel)
	SystemInstructionsKey         = attribute.Key(SystemInstructions)
	InputMessagesKey              = attribute.Key(InputMessages)
	OutputMessagesKey             = attribute.Key(OutputMessages)
	InputTokensKey                = attribute.Key(InputTokens)
	OutputTokensKey               = attribute.Key(OutputTokens)
	CacheCreationInputTokensKey   = attribute.Key(CacheCreationInputTokens)
	CacheReadInputTokensKey       = attribute.Key(CacheReadInputTokens)
	TokenTypeKey                  = attribute.Key(TokenType)
	ToolNameKey                   = attribute.Key(ToolName)
	ToolCallIDKey                 = attribute.Key(ToolCallID)
	ToolCallArgumentsKey          = attribute.Key(ToolCallArguments)
	ToolCallResultKey             = attribute.Key(ToolCallResult)
	ToolErrorKey                  = attribute.Key(ToolError)
	EvaluationScoreKey            = attribute.Key(EvaluationScore)
	EvaluationTextKey             = attribute.Key(EvaluationText)
	EvaluationMetricNameKey       = attribute.Key(EvaluationMetricName)
	EvaluationReasoningKey        = attribute.Key(EvaluationReasoning)
	ServerAddressKey              = attribute.Key(ServerAddress)
	ServerPortKey                 = attribute.Key(ServerPort)
	ErrorTypeKey                  = attribute.Key(ErrorType)
	UsageCostKey                  = attribute.Key(UsageCost)
	CostCurrencyKey               = attribute.Key(CostCurrency)
	UsageReasoningOutputTokensKey = attribute.Key(UsageReasoningOutputTokens)
	UsageVideoSecondsKey          = attribute.Key(UsageVideoSeconds)
	UsageVideoFramesKey           = attribute.Key(UsageVideoFrames)
	AudioSecondsKey               = attribute.Key(AudioSeconds)
	ImageCountKey                 = attribute.Key(ImageCount)
	OperationPurposeKey           = attribute.Key(OperationPurpose)
	RetrievalSourceKey            = attribute.Key(RetrievalSource)
	RetrievalProviderKey          = attribute.Key(RetrievalProvider)
	RetrievalQueryKey             = attribute.Key(RetrievalQuery)
	RetrievalTopKKey              = attribute.Key(RetrievalTopK)
	RetrievalReturnedChunksKey    = attribute.Key(RetrievalReturnedChunks)
	RetrievalDistancesKey         = attribute.Key(RetrievalDistances)
	SamplingKeepKey               = attribute.Key(SamplingKeep)
	CacheHitKey                   = attribute.Key(CacheHit)
	EmbeddingModelKey             = attribute.Key(EmbeddingModel)
	AgentNameKey                  = attribute.Key(AgentName)
	AgentRoleKey                  = attribute.Key(AgentRole)
	WorkflowStepKey               = attribute.Key(WorkflowStep)
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
