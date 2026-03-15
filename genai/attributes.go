// Package genai provides OpenLLMetry semantic convention constants and helpers
// for recording GenAI usage (tokens, cost, prompts) on OTel spans.
package genai

import "go.opentelemetry.io/otel/attribute"

// OpenLLMetry semantic convention attribute keys.
// See https://openllmetry.io/ and OpenTelemetry GenAI semantic conventions.
//
// Note on OpenTelemetry standard evolution:
// Currently, metry defines its own semantic conventions for GenAI (e.g. CostUSD = "gen_ai.usage.cost")
// tracking the OpenLLMetry project. The OpenTelemetry project is actively standardizing
// GenAI semantic conventions (semconv). When official OTel GenAI semconv mature and are
// included in the standard Go OTel packages (e.g., > 1.42), these constants should
// be updated to alias the official ones to ensure long-term ecosystem compatibility
// without breaking the public `metry/genai` API.
const (
	System        = "gen_ai.system"
	RequestModel  = "gen_ai.request.model"
	ResponseModel = "gen_ai.response.model"
	Prompt        = "gen_ai.prompt"
	Completion    = "gen_ai.completion"
	InputTokens   = "gen_ai.usage.input_tokens"  // #nosec G101 -- OTel semantic convention name, not a credential
	OutputTokens  = "gen_ai.usage.output_tokens" // #nosec G101 -- OTel semantic convention name, not a credential
	CostUSD       = "gen_ai.usage.cost"

	// Tool Calling (toolsy).
	ToolName   = "gen_ai.tool.name"
	ToolID     = "gen_ai.tool.id"
	ToolArgs   = "gen_ai.tool.args"   // Expected JSON string.
	ToolResult = "gen_ai.tool.result" // Tool output (truncated if over internal limit).
	ToolError  = "gen_ai.tool.error"  // True if the tool call failed.

	// RAG and Semantic Cache.
	RetrievalSource = "gen_ai.retrieval.source"
	CacheHit        = "gen_ai.cache.hit"
	EmbeddingModel  = "gen_ai.embedding.model"

	// Multi-agent and orchestration (flowy).
	AgentName    = "gen_ai.agent.name"
	AgentRole    = "gen_ai.agent.role"
	WorkflowStep = "gen_ai.workflow.step"
	PromptType   = "gen_ai.prompt.type"

	// Operation purpose for cost tracking (e.g. generation vs guard evaluation).
	OperationPurpose = "ai.operation.purpose"
)

// Attribute keys as attribute.Key for type-safe span recording.
var (
	SystemKey        = attribute.Key(System)
	RequestModelKey  = attribute.Key(RequestModel)
	ResponseModelKey = attribute.Key(ResponseModel)
	PromptKey        = attribute.Key(Prompt)
	CompletionKey    = attribute.Key(Completion)
	InputTokensKey   = attribute.Key(InputTokens)
	OutputTokensKey  = attribute.Key(OutputTokens)
	CostUSDKey       = attribute.Key(CostUSD)

	ToolNameKey   = attribute.Key(ToolName)
	ToolIDKey     = attribute.Key(ToolID)
	ToolArgsKey   = attribute.Key(ToolArgs)
	ToolResultKey = attribute.Key(ToolResult)
	ToolErrorKey  = attribute.Key(ToolError)

	RetrievalSourceKey = attribute.Key(RetrievalSource)
	CacheHitKey        = attribute.Key(CacheHit)
	EmbeddingModelKey  = attribute.Key(EmbeddingModel)

	AgentNameKey    = attribute.Key(AgentName)
	AgentRoleKey    = attribute.Key(AgentRole)
	WorkflowStepKey = attribute.Key(WorkflowStep)
	PromptTypeKey   = attribute.Key(PromptType)

	// Operation purpose for cost tracking (e.g. generation vs guard evaluation).
	OperationPurposeKey = attribute.Key(OperationPurpose)
)

// Standard values for OperationPurpose.
const (
	PurposeGeneration        = "generation"
	PurposeGuardEvaluation   = "guard_evaluation"
	PurposeQualityEvaluation = "quality_evaluation"
)
