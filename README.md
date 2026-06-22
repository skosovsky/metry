# metry

[![Go](https://img.shields.io/badge/Go-%3E%3D1.26-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-100%25-000000?logo=opentelemetry)](https://opentelemetry.io/)

Universal, low-boilerplate OpenTelemetry and GenAI telemetry toolkit for Go.

## Installation

```bash
go get github.com/skosovsky/metry
go get github.com/skosovsky/metry/middleware/grpc
```

Use matching versions of `metry` and `metry/middleware/grpc` after the stateless API break.

**Migration (task16):** use `genai.Runtime`, explicit `PayloadPolicy`, `TraceSnapshot` for durable resume, runtime-owned async outcomes, and typed tool error classes. Historical task14 notes live in [`.cursor/docs/task14-migration.md`](.cursor/docs/task14-migration.md), but task16 supersedes carrier-map queue guidance and narrow recorder wiring.

`middleware/http` and [`middleware/executor`](middleware/executor) are thin aliases for root APIs `metry.HTTPHandler` and `metry.ExecutorWrap`. Prefer the root imports when starting new code; subpackages remain for stable import paths.

## Quick start

```go
package main

import (
    "context"

    "github.com/skosovsky/metry"
    "github.com/skosovsky/metry/genai"
)

func setup(ctx context.Context) (*metry.Provider, genai.Runtime, error) {
    provider, err := metry.New(
        ctx,
        metry.WithServiceName("my-ai-service"),
        metry.WithTraceRatio(1.0),
        // Optional: override ratio with a custom head sampler.
        // metry.WithSampler(genai.NewHintSampler(metry.TraceIDRatioBased(0.1))),
        // metry.WithExporter(...),
        // metry.WithMetricExporter(...),
    )
    if err != nil {
        return nil, nil, err
    }

    runtime, err := genai.NewRuntimeFromProvider(provider, genai.WithPayloadPolicy(genai.RedactPayloadPolicy()))
    if err != nil {
        _ = provider.Shutdown(ctx)
        return nil, nil, err
    }

    return provider, runtime.ForProvider("provider"), nil
}
```

`metry.New(...)` is stateless and does not mutate global OTel providers.
`WithSampler(...)` is head-based and takes precedence over `WithTraceRatio(...)`.

## Provider lifecycle

```go
provider, runtime, err := setup(ctx)
if err != nil {
    return err
}
defer provider.Shutdown(ctx)

ctx, end, err := provider.StartSpan(ctx, "app/handler", "request")
if err != nil {
    return err
}
defer end()

err = runtime.RecordOperation(ctx,
    genai.Operation{
        Name:    "chat",
        Model:   "model",
        Purpose: genai.PurposeGeneration,
    },
    genai.OperationResult{
        Status: genai.OperationStatusOK,
        Usage:  genai.Usage{InputTokens: 150, OutputTokens: 50},
        Payload: genai.Payload{
            InputMessages: []genai.Message{{
                Role: "user",
                Parts: []genai.ContentPart{{Type: "text", Content: "Summarize this"}},
            }},
        },
    },
)
if err != nil {
    return err
}
```

`Runtime.RecordOperation` creates its own span, writes standard GenAI attributes and metrics, and applies the configured payload policy before export.

Raw payload export requires `genai.WithRawPayloads()`. Use it only when the application has already handled privacy before calling `metry`.

Extra `attrs ...metry.Attribute` on `RecordOperation` are for bounded metadata and outcome fields. Do not put prompts, completions, tool arguments, or raw user data there; payload policy applies to `genai.Payload` and tool result types.

## GenAI API

Use `genai.Runtime` as the application boundary:

- `RecordOperation(ctx, Operation, OperationResult, attrs...)`
- `RecordTraceIO(ctx, input, output)`
- `RecordTTFT(ctx, duration)`
- `RecordStreamingCompletion(ctx, StreamingCompletion)`
- `StartTool(ctx, ToolCall, opts...)`
- `RecordToolResult(ctx, ToolResult)`
- `StartAsync(ctx, name, opts...)`
- `RecordAsyncResult(ctx, AsyncHandle, AsyncResult)`
- `RecordAsyncTokenResult(ctx, token, AsyncResult)`

`genai.NoopRuntime()` and `genai.RuntimeFromProvider(...)` are safe for optional telemetry wiring.

Application/business code should use `Runtime`; do not build a local compatibility facade over lower-level tracker methods. Bind provider context in the composition root with `runtime.ForProvider(...)` so business services record operation intent, not vendor labels. Framework code that owns SDK translation can use adapter-level APIs behind its own package boundary.

## Payload policy

Payload recording is policy-driven:

```go
runtime, err := genai.NewRuntimeFromProvider(provider, genai.WithPayloadPolicy(genai.RedactPayloadPolicy()))
if err != nil {
    return err
}
runtime = runtime.ForProvider("provider")
```

- `RedactPayloadPolicy()` preserves message/tool structure and replaces content with redacted markers.
- `HashPayloadPolicy()` preserves structure and exports deterministic fingerprints.
- `WithRawPayloads()` is the explicit opt-in for raw payloads.

The same policy applies to messages, tool arguments/results, retrieval queries, feedback text, and evaluation reasoning.

## Tool lifecycle

```go
toolCtx, endTool := runtime.StartTool(ctx, genai.ToolCall{
    Name:      "search",
    CallID:    callID,
    Arguments: argsJSON,
})
defer endTool()

result, err := runTool(toolCtx)
runtime.RecordToolResult(toolCtx, genai.ToolResult{
    Result: resultJSON,
    Err:    err,
})
```

Tool lifecycle writes a duration metric with bounded labels: `tool`, `status`, `error_type`. `status` is normalized to `ok`, `error`, or `unset`; unknown error details collapse to `unknown`. Pass a stable tool name, not a prompt, query, user id, or runtime payload. Use `WithToolErrorClassifier(...)` and `WithAllowedToolErrorClasses(...)` when adapter code needs an explicit bounded taxonomy.

## Unified observability context (Enrich)

Use typed attributes once; metry mirrors them to baggage, the active span, and slog context:

```go
logger := slog.New(metry.ContextHandler{Handler: slog.NewJSONHandler(os.Stdout, nil)})
ctx = metry.Enrich(ctx, metry.TenantID("t-1"), metry.PatientID("p-9"))
logger.InfoContext(ctx, "request started")
```

Each `Enrich` call returns an updated context. **Always reassign** `ctx` when adding attributes across multiple calls — baggage accumulates only on the returned context:

```go
ctx = metry.Enrich(ctx, metry.TenantID("t-1"))
ctx = metry.Enrich(ctx, metry.PatientID("p-9")) // required for baggage
```

`ContextHandler` logs only metry enrich attributes (members with `metry.attr.type` metadata), not foreign baggage keys.

`SetBaggageValue` / raw string baggage keys are removed (clear break). Use `metry.Enrich` and semantic constructors (`TenantID`, `PatientID`, `DoctorID`, `SubjectID`).

## Trace snapshot for durable resume

Store opaque trace continuation tokens in queue/checkpoint payloads instead of carrier maps:

```go
snapshot, err := metry.TraceSnapshotFromContext(ctx)
if err != nil {
    return err
}
token, err := snapshot.Marshal()
if err != nil {
    return err
}
// enqueue token...

parsed, err := metry.ParseTraceSnapshot(token)
if err != nil {
    return err
}
consumerCtx, err := provider.ContextWithTraceSnapshot(context.Background(), parsed)
if err != nil {
    return err
}
_, end, err := provider.StartSpan(consumerCtx, "app", "consumer")
if err != nil {
    return err
}
defer end()
```

`TraceSnapshot` carries trace continuation only. It does not capture baggage or application fields.

## Adapter-level trace context propagation (map carrier)

Use map carriers only at protocol or middleware boundaries where a W3C carrier map already exists. Do not store carrier maps in domain state, queue payloads, or checkpoints; use `TraceSnapshot` for durable resume.

```go
headers := map[string]any{"x-request-id": requestID}
provider.InjectToMap(ctx, headers)
// hand headers to a protocol/middleware adapter...

consumerCtx := provider.ExtractFromMap(context.Background(), headers)
_, end, err := provider.StartSpan(consumerCtx, "app", "consumer")
if err != nil {
    return err
}
defer end()
```

Host business keys in the map are preserved; W3C trace context and OTel baggage fields are read on extract (per the provider propagator). This is a low-level adapter API, not the application durable-continuation path.

## Deferred outcomes (AsyncHandle)

Capture a portable handle at enqueue time and record linked outcomes in workers without storing `SpanContext` or tunneling tokens through `context.Context`:

```go
handle, err := runtime.StartAsync(ctx, "queue.delivery")
token, err := handle.Marshal()
// enqueue token...

err = runtime.RecordAsyncTokenResult(ctx, token, genai.AsyncResult{
    Name:   "delivery.success",
    Status: genai.OperationStatusOK,
    Attributes: []metry.Attribute{
        metry.TenantID("t-1"),
    },
})
```

Adapter helpers for async feedback and evaluations still accept `metry.AsyncHandle` (portable marshal token), but application code should prefer the runtime-owned async path above.

For delayed GenAI feedback, adapter code captures a handle at request time and passes the same handle token to the worker-side adapter helper. Feedback recording requires a valid handle (`genai.ErrInvalidAsyncHandle` otherwise). Correlation uses Span Links, not parent-child hierarchy.

Advanced: optional typed span options (e.g. `genai.WithSamplingKeep()`) do not require importing `go.opentelemetry.io/otel/trace`.

## Domain metrics registry

Register histograms, counters, and gauges at init; record at runtime with typed labels (no `go.opentelemetry.io/otel/metric` in host code):

```go
registry := metry.NewMetricsRegistry(provider)
duration, err := registry.NewHistogram("agent_loop_duration", []float64{0.5, 1, 2, 4, 8})
steps, err := registry.NewCounter("agent_loop_steps_total")
queueDepth, err := registry.NewGauge("queue_depth")
if !duration.OK() || !steps.OK() || !queueDepth.OK() {
    // registry not configured or New* failed — do not use zero-value metric wrappers
}
duration.Record(ctx, elapsed.Seconds(), metry.Labels{"status": "ok"})
steps.Add(ctx, 1, metry.Labels{"status": "ok"})
queueDepth.Record(ctx, float64(len(queue)), metry.Labels{"queue": "jobs"})
```

**Footgun:** zero-value `HistogramMetric{}`, `CounterMetric{}`, or `GaugeMetric{}` silently no-op on `Record`/`Add`. Always check `OK()` after `NewHistogram`/`NewCounter`/`NewGauge`, or keep the returned error.

**Footgun (duplicate names):** metric names are unique across instrument types — registering the same name as histogram and counter returns `ErrDuplicateMetric`.

**Footgun (labels):** empty keys and empty values are skipped in `LabelsOf`, `copyLabels`, and metric attribute conversion.

**Footgun (GenAI scope metrics):** token/TTFT/streaming histograms require an `Operation` in scope or `Meta`. Provider is bound by `Runtime.ForProvider(...)` or defaults to `unknown`; do not push provider labels through business scope just to make metrics appear.

**Footgun (payload policy):** payload recording is off until you pass `WithPayloadPolicy(...)` or `WithRawPayloads()`. Use raw export only after application-level privacy handling.

**Footgun (GenAI errors):** `Meta.ErrorType` sets span status to Error in addition to the `error.type` attribute. `Runtime.RecordToolResult(ctx, ToolResult{Err: err, ErrorClass: kind})` uses bounded `error_type` for tool metrics. Pass `nil` on success, and register custom classes with `WithAllowedToolErrorClasses(...)`.

**Footgun (propagation):** `Provider.InjectToMap` / `ExtractFromMap` are for protocol-level carriers. For queue payloads, checkpoints, and durable resume, use `TraceSnapshot`.

**Footgun (AsyncHandle):** marshal tokens are not signed; treat queue payloads as trusted or add application-level signing for untrusted brokers.

**BaggageMember** is a read/debug helper. Prefer typed constructors (`TenantID`, `GenAIProvider`, etc.) when writing context via `Enrich`.

**Typed baggage:** use `metry.IntAttribute`, `FloatAttribute`, and `BoolAttribute` for non-string enrich keys; `ContextHandler` restores typed slog fields when baggage carries `metry.attr.type` metadata across protocol propagation.

## Adapter-only GenAI APIs

Application code should stay on `genai.Runtime`. Lower-level tracker APIs exist for framework or SDK adapters that must translate vendor-specific request/response types into canonical `genai.Payload`, `genai.Meta`, and `genai.Usage`. Do not wrap them into a local compatibility facade; move adapter code to the boundary and expose `Runtime` to the host application.

## Sampling hints (head-based)

```go
runtime, err := genai.NewRuntimeFromProvider(provider)
if err != nil {
    return err
}

_, end := runtime.StartTool(ctx, genai.ToolCall{Name: "retrieval"}, genai.WithSpanSamplingKeep())
defer end()
```

The keep hint is evaluated at span start in the SDK sampler.
It does not depend on post-hoc status, token usage, or tail sampling.
Without keep hint, sampled parent context is inherited; the base sampler is consulted for new root spans.
Adapter helpers that create spans internally also accept typed options. Application code normally uses `Runtime.StartTool(...)`; adapter code can pass the same hint through retrieval, feedback, or evaluation helpers via `genai.WithSpanSamplingKeep()` or `genai.WithSamplingKeep()`.
When caller options contain duplicate attribute keys, helper built-in keys win.

## Adapter helper: retrieval spans

Use this in retriever adapters that need dedicated retrieval spans. Application code should still use `Runtime` for the main GenAI operation. The callback style keeps host code away from `trace.Span`, `otel/codes`, and manual status handling:

```go
retrievalCtx, end := adapterTracker.StartRetrievalSpan(ctx, "vector.search", genai.RetrievalRequest{
    Provider: "qdrant",
    Source:   "knowledge_base",
    Query:    query,
    TopK:     5,
}, genai.WithSpanSamplingKeep())
defer end()

chunks, distances, err := retriever.Search(retrievalCtx, query)
if err != nil {
    return err
}

adapterTracker.RecordRetrievalResult(retrievalCtx, genai.RetrievalResult{
    ReturnedChunks: len(chunks),
    Distances:      distances,
})
```

## Adapter helper: linked evaluations

Evaluations follow the same link-based model for delayed/post-hoc adapter work:

```go
if err := adapterTracker.RecordEvaluations(
    ctx,
    parsed,
    []genai.Evaluation{
        {
            Metric:    genai.EvaluationFaithfulness,
            Score:     0.91,
            Reasoning: "Grounded in retrieved chunks.",
        },
        {
            Metric: genai.EvaluationAnswerRelevance,
            Score:  0.84,
        },
    },
    genai.WithSamplingKeep(),
); err != nil {
    return err
}
```

`metry` does not provide trace mutation / PATCH APIs for updating finished spans—use Span Links for post-hoc feedback and evaluations.

## Payload truncation

Payload/tool attributes are written as strings and truncated with UTF-8-safe truncation.

- Default max length: `65536` bytes.
- Configure via `genai.WithMaxContextLength(bytes)`.
- Span **event** text (e.g. LLM-judge reasoning on evaluation events when payloads are enabled) uses a separate, smaller default (`4096` bytes) via `genai.WithMaxEventLength`, because backends often cap event attributes more strictly than span attributes.
- Truncated values may be non-valid JSON strings by design.

## HTTP middleware

```go
import metryhttp "github.com/skosovsky/metry/middleware/http"

handler := metryhttp.Handler(provider, next, "http.request")
```

## Generic execution telemetry

Use `metry.ExecutorWrap` (or the thin alias `middleware/executor.Wrap`) to instrument any function with shape `func(context.Context, Req) (Res, error)` (database calls, LLM clients, internal pipelines) without tying it to HTTP or gRPC.

**Logging:** structured logs (start, errors, panics) are emitted only when you pass `slog` via `executor.WithLogger`. Without a logger you still get **traces and metrics** only. Start and error/panic lines include `trace_id` when the span has a W3C trace id. **Request/response bodies are never logged** on purpose (PII / payload safety); this is library policy, not an omission.

**Options:** `WithLogStart` / `WithLogError` map to optional telemetry in the task spec; there is no response-body logging (`LogResponses`) unless introduced in a separate change.

```go
import metryexec "github.com/skosovsky/metry/middleware/executor"

run := metryexec.Wrap(provider, "my.pipeline.step", func(ctx context.Context, req MyReq) (MyRes, error) {
    return doWork(ctx, req)
})
res, err := run(ctx, req)
```

Many frameworks expose `Execute(ctx, req) (res, err)` behind an interface. You can adapt `Wrap` without importing that framework:

```go
type Executor[Req, Res any] interface {
    Execute(ctx context.Context, req Req) (Res, error)
}

type ExecutorFunc[Req, Res any] func(context.Context, Req) (Res, error)

func (f ExecutorFunc[Req, Res]) Execute(ctx context.Context, req Req) (Res, error) {
    return f(ctx, req)
}

wrapped := metryexec.Wrap(provider, "my_op", myLogic)
var exec Executor[MyReq, MyRes] = ExecutorFunc[MyReq, MyRes](wrapped)
res, err := exec.Execute(ctx, req)
```

Metrics use meter scope `github.com/skosovsky/metry/middleware/executor`:

- `executor_operation_duration` — histogram of wall time in seconds (`operation`, `status`).
- `executor_operation_calls` — invocation counter with the same attributes (`status` is `success`, `error`, or `panic`).

A minimal runnable sample lives in `examples/executor`.

Primary examples (run via `make test-examples`):

| Example                     | Demonstrates                                                              |
| --------------------------- | ------------------------------------------------------------------------- |
| `examples/enrich`           | `Enrich` + `ContextHandler` slog correlation                              |
| `examples/async_handle`     | `TraceSnapshot` + runtime-owned async linked outcome                      |
| `examples/metrics_registry` | `TraceSnapshot` + registry metrics without OTel metric imports            |
| `examples/executor`         | Root `metry.ExecutorWrap` for generic executors                           |
| `examples/genai_recorder`   | `genai.Runtime` + payload policy + trace snapshot + tool lifecycle        |

Adapter-level examples, kept to exercise low-level APIs:

| Example                    | Demonstrates                                                   |
| -------------------------- | -------------------------------------------------------------- |
| `examples/propagation_map` | Protocol-level `InjectToMap` / `ExtractFromMap` carrier bridge |
| `examples/scope`           | `genai.WithScope` with explicit TraceSnapshot worker rehydrate |

## gRPC middleware

```go
import metrygrpc "github.com/skosovsky/metry/middleware/grpc"

server := grpc.NewServer(metrygrpc.ServerOptions(
    provider,
)...)
conn, err := grpc.NewClient(addr, metrygrpc.ClientDialOption(
    provider,
))
```

## Testing

`make test` runs tests for all modules. `make test-examples` runs the examples above.

## License

MIT
