# metry

[![Go](https://img.shields.io/badge/Go-%3E%3D1.26-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-100%25-000000?logo=opentelemetry)](https://opentelemetry.io/)

**Universal, zero-boilerplate OpenTelemetry & LLMOps hub for Go AI applications. One line of code to trace them all.**

---

## Why metry

- **Zero-Boilerplate Init** — Configure Tracer, Meter, and W3C propagators in a single call. No OTel SDK setup boilerplate.
- **100% Vendor-Agnostic** — Bring your own OTel exporters and transports. `metry` configures providers and GenAI helpers without forcing OTLP gRPC or any specific backend.
- **Official OTel GenAI + pragmatic extensions** — Emits official OpenTelemetry GenAI attributes and metrics where the spec already exists, and keeps explicit custom extensions for cost and streaming UX metrics.
- **Plug-and-Play Middlewares** — Ready-made wrappers for `net/http`, plus optional gRPC integration published as a separate module.

## Architecture

```mermaid
graph LR
  subgraph Libraries
    ragy[ragy]
    flowy[flowy]
    toolsy[toolsy]
    app[your app]
  end

  subgraph metryPkg [metry]
    metryCore[Init - global OTel providers]
    metryGenAI[genai]
  end

  subgraph metryIntegrations [integrations]
    metryHTTP[HTTP middleware]
    metryGRPC[gRPC middleware module]
  end

  subgraph Backends
    langfuse[Langfuse]
    jaeger[Jaeger]
    tempo[Grafana Tempo]
  end

  ragy --> metryCore
  flowy --> metryCore
  toolsy --> metryCore
  app --> metryCore
  app --> metryGenAI
  app --> metryHTTP
  app --> metryGRPC
  metryCore --> langfuse
  metryCore --> jaeger
  metryCore --> tempo
```

## Installation

```bash
go get github.com/skosovsky/metry
```

Optional gRPC integration:

```bash
go get github.com/skosovsky/metry/middleware/grpc
```

## Development

For repo-local validation, use the Make targets instead of invoking `golangci-lint` directly:

```bash
make fmt
make lint
make test
make test-race
```

These targets are module-aware (`metry` + `middleware/grpc`), apply the repository's `goimports` local-prefix rules, and use cache locations that work reliably with the nested-module workspace layout.

## Quick Start

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/skosovsky/metry"
	"github.com/skosovsky/metry/genai"
	metryhttp "github.com/skosovsky/metry/middleware/http"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

func main() {
	ctx := context.Background()

	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint("localhost:4318"),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		log.Fatal(err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint("localhost:4318"),
		otlpmetrichttp.WithInsecure(),
	)
	if err != nil {
		log.Fatal(err)
	}

	shutdown, err := metry.Init(ctx,
		metry.WithServiceName("my-ai-service"),
		metry.WithEnvironment("production"),
		metry.WithExporter(traceExporter),
		metry.WithMetricExporter(metricExporter),
		metry.WithGenAIConfig(
			genai.WithRecordPayloads(false), // privacy-first default
		),
		metry.WithTraceRatio(1.0),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer shutdown(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := metryhttp.Handler(mux, "HTTP /")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
```

`metry` no longer creates OTLP or gRPC exporters internally. Construct exporters in your application and inject them with `WithExporter` / `WithMetricExporter`. When `WithMetricExporter` is omitted, the current `metry.Init(...)` session installs a no-op meter provider, so GenAI metrics are not exported and are not inherited from a previous metry session.

## Semantic Conventions (LLMOps)

Use `otel.Tracer("your-service/module")` and `otel.Meter("your-service/module")` for instrumentation scope so dashboards can filter traces and metrics by module.

`metry` emits official OTel GenAI span attributes and client metrics where they exist, including `gen_ai.provider.name`, `gen_ai.operation.name`, `gen_ai.input.messages`, `gen_ai.output.messages`, `gen_ai.client.token.usage`, and `gen_ai.client.operation.duration`. Cost, token-component breakdown, and streaming UX signals remain explicit custom extensions. Payload recording is opt-in and configured via `metry.WithGenAIConfig(...)`.

```go
import (
	"time"

	"github.com/skosovsky/metry/genai"
	"go.opentelemetry.io/otel"
)

// Use a tracer named after your service/module for granular filtering in Jaeger/Grafana:
ctx, span := otel.Tracer("my-ai-service/llm").Start(ctx, "llm-call")
defer span.End()

// After the LLM responds:
genai.RecordInteraction(ctx, span,
	genai.GenAIMeta{
		Provider:      "openai",
		Operation:     "chat",
		RequestModel:  "gpt-4o-mini",
		ResponseModel: "gpt-4o-mini",
		Duration:      850 * time.Millisecond,
	},
	genai.GenAIPayload{
		SystemInstructions: []genai.GenAIContentPart{{
			Type:    "text",
			Content: "You are a concise assistant.",
		}},
		InputMessages: []genai.GenAIMessage{{
			Role: "user",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: "Summarize this",
			}},
		}},
		OutputMessages: []genai.GenAIMessage{{
			Role: "assistant",
			Parts: []genai.GenAIContentPart{{
				Type:    "text",
				Content: "Here is the summary...",
			}},
			FinishReason: "stop",
		}},
	},
		genai.GenAIUsage{
			// InputTokens should include cached input tokens when the provider exposes a total.
			InputTokens: 150,
			// OutputTokens should include reasoning tokens when the provider exposes a total.
			OutputTokens: 50,
			// Cost must be non-negative. Negative values are treated as invalid input and ignored.
			Cost:         0.002,
			// Currency is optional; when empty, metry records "USD".
			Currency: "USD",
		},
	)
```

On spans, structured payload attributes are serialized as JSON strings because Go OTel attributes do not support arbitrary structured values directly. `metry` normalizes payload and tool JSON before emitting it, guarantees that truncated JSON-valued attrs stay valid JSON, and drops malformed tool JSON instead of writing invalid semconv values. `GenAIUsage.Cost` is treated as a non-negative input; negative values are ignored and do not emit span attrs or cost metrics. Long payload and tool JSON strings are truncated to 16 KB by default; override this with `metry.WithGenAIConfig(genai.WithMaxContextLength(bytes))`.

To record failures on a span in a consistent way (e.g. in HTTP/gRPC handlers or after a failed LLM call), use `traceutil.SpanError` so the span gets the error recorded and status set to Error:

```go
import "github.com/skosovsky/metry/traceutil"

// In a handler, before span.End():
if err != nil {
	traceutil.SpanError(span, err)
}
defer span.End()
```

`metry.Init` installs the package default tracker used by package-level convenience wrappers. Supported wrappers are `genai.RecordInteraction`, `genai.RecordTTFT`, `genai.RecordStreamingCompletion`, `genai.RecordAsyncFeedback`, `genai.StartToolSpan`, and `genai.RecordToolResult`. Stateless helpers that do not depend on tracker runtime remain package-level by design: `genai.RecordCacheHit` and `genai.RecordAgentStep`.

Advanced users can create trackers with `genai.NewTracker(...)` when the ambient tracer is acceptable, or pass an explicit tracer with `genai.NewTrackerWithTracer(...)` when they need tool spans and metrics to stay bound to a specific tracer provider.

Before `metry.Init`, the package default tracker stays privacy-first: payload recording is off, and helper-created spans (`genai.StartToolSpan` and `genai.RecordAsyncFeedback`) use a no-op tracer instead of silently writing to any ambient global provider.

## Agentic & RAG Tracing

Each tool call gets its own **child span** so parallel tool invocations have correct timing in Jaeger/Tempo. Start a tool span before execution and record the result on that span; use **events** for agent steps so ReAct loops (Thought -> Action -> Observation) appear as a chronological list:

```go
// Start a child span for the tool (caller MUST call span.End(), e.g. via defer):
ctx, span := genai.StartToolSpan(ctx, "search", "call-1", `{"q":"weather"}`)
defer span.End()

// After the tool returns (result or error), record on the same span:
genai.RecordToolResult(span, `{"temp":22}`, false)

// After checking semantic cache in RAG layer:
genai.RecordCacheHit(span, true, "pgvector_cache")

// When transitioning workflow steps (e.g. in flowy); each call adds an event (no overwrite).
// Event name genai.AgentStepEvent follows OTel GenAI semantic conventions.
genai.RecordAgentStep(span, "cardiologist", "specialist", "step-2")
```

## Streaming & UX Metrics

Record client-side Time To First Token (TTFT) for streaming LLM responses. Pass the same `GenAIMeta` you use for usage metrics:

```go
start := time.Now()
meta := genai.GenAIMeta{
	Provider:     "openai",
	Operation:    "chat",
	RequestModel: "gpt-4o",
}
// ... start streaming, receive first token ...
ttft := time.Since(start)
genai.RecordTTFT(ctx, meta, ttft)
```

The custom `metry.gen_ai.client.ttft` histogram is exported with the same provider/operation/model dimensions as the rest of the GenAI tracker. Official token totals continue to use `gen_ai.client.token.usage`, while cache/reasoning breakdown is exported separately via the custom `metry.gen_ai.client.token.component.usage` histogram.

After the stream finishes, record aggregate streaming performance:

```go
// ttft measured earlier, totalDuration is end-to-end stream duration.
genai.RecordStreamingCompletion(ctx, meta, 256, ttft, totalDuration)
```

## Context Propagation (Baggage)

Propagate key-value metadata (e.g. `session_id`, `patient_id`) across HTTP and gRPC boundaries. Keys and values must comply with W3C Baggage; invalid key/value returns a wrapped error.

```go
// At entry point (e.g. after auth):
ctx, err := metry.SetBaggageValue(ctx, "patient_id", "p-123")
if err != nil {
	// invalid key/value (e.g. spaces, special chars)
}

// Downstream (any service receiving the context):
id := metry.BaggageValue(ctx, "patient_id") // "p-123"
```

## Security Observability

Use the `security` package to record security interventions (e.g. PII masking, LLM judges, shadow mode) as span events and to tag spans with `ai.security.*` attributes for dashboards.

```go
import "github.com/skosovsky/metry/security"

// Record an intervention as an event on the current span (e.g. from middleware):
security.RecordSecurityEvent(ctx, security.ActionBlock, "pii_masking", "PII detected in prompt", false)

// Tag the whole security pipeline span (e.g. for Grafana):
span.SetAttributes(
	security.ShadowModeKey.Bool(true),
	security.ValidatorKey.String("llm_judge"),
	security.ActionKey.String(security.ActionPass),
)
```

| Attribute | Description |
|-----------|-------------|
| `ai.security.tier` | Protection tier (e.g. 1, 2, 3). |
| `ai.security.validator` | Name of the filter (e.g. `pii_masking`, `llm_judge`, `vector_firewall`). |
| `ai.security.action` | Decision: `pass`, `block`, `redact`. Use `security.ActionPass`, `security.ActionBlock`, `security.ActionRedact`. |
| `ai.security.shadow_mode` | If `true`, blocking was virtual (shadow mode). |
| `ai.security.score` | Confidence or cosine distance for semantic checks. |
| `ai.security.reason` | Human-readable reason for block or mutation. |

To separate guard-evaluation cost from user-facing generation in billing and dashboards, pass an explicit `Purpose` inside `genai.GenAIUsage`:

```go
// Normal reply to the user (Purpose defaults to "generation" when empty):
genai.RecordInteraction(ctx, span, meta, genai.GenAIPayload{}, genai.GenAIUsage{
	InputTokens:  150,
	OutputTokens: 50,
	Cost:         0.002,
	Currency:     "USD",
})

// LLM-judge / guard evaluation — same metrics, split by purpose and currency:
genai.RecordInteraction(ctx, span, meta, genai.GenAIPayload{}, genai.GenAIUsage{
	InputTokens:  20,
	OutputTokens: 5,
	Cost:         0.0003,
	Currency:     "CREDITS",
	Purpose:      genai.PurposeGuardEvaluation,
})
```

The `gen_ai.cost` metric is exported as a counter and includes `ai.operation.purpose` plus
`gen_ai.cost.currency`. When `Currency` is omitted, metry records `USD`.

## HTTP and gRPC

- **HTTP** — Wrap your handler: `metryhttp.Handler(mux, "operation-name")`.
- **gRPC** — Install `github.com/skosovsky/metry/middleware/grpc` separately, then use `metrygrpc.ServerOptions()` and `metrygrpc.ClientDialOption()`.

## Ecosystem

metry is the central observability layer for the AI stack. It makes libraries such as ragy (RAG), flowy (orchestration), and toolsy (tools) visible in production by configuring global OTel providers and standard GenAI attributes. Use `otel.Tracer("your-module")` and `otel.Meter("your-module")` for granular instrumentation scope.

## Contributing

Contributions are welcome. Please open an issue or PR.

## License

MIT. See [LICENSE](LICENSE) for details.
