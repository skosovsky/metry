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

**Migration (task14):** see [docs/task14-migration.md](docs/task14-migration.md) for breaking changes (Enrich, map propagation, AsyncHandle, Scope, MetricsRegistry).

`middleware/http` and [`middleware/executor`](middleware/executor) are thin aliases for root APIs `metry.HTTPHandler` and `metry.ExecutorWrap`. Prefer the root imports when starting new code; subpackages remain for stable import paths.

## Quick start

```go
package main

import (
    "context"

    "github.com/skosovsky/metry"
    "github.com/skosovsky/metry/genai"
)

func setup(ctx context.Context) (*metry.Provider, *genai.Tracker, error) {
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

    tracker, err := genai.NewTrackerFromProvider(provider, genai.WithRecordPayloads(true))
    if err != nil {
        _ = provider.Shutdown(ctx)
        return nil, nil, err
    }

    return provider, tracker, nil
}
```

`metry.New(...)` is stateless and does not mutate global OTel providers.
`WithSampler(...)` is head-based and takes precedence over `WithTraceRatio(...)`.

## Provider lifecycle

```go
provider, tracker, err := setup(ctx)
if err != nil {
    return err
}
defer provider.Shutdown(ctx)

ctx, end, err := provider.StartSpan(ctx, "app/handler", "request")
if err != nil {
    return err
}
defer end()

err = tracker.RecordInteraction(ctx,
    genai.Meta{
        Provider:      "openai",
        Operation:     "chat",
        RequestModel:  "gpt-4o-mini",
        ResponseModel: "gpt-4o-mini",
    },
    genai.Payload{
        InputMessages: []genai.Message{{
            Role: "user",
            Parts: []genai.ContentPart{{Type: "text", Content: "Summarize this"}},
        }},
    },
    genai.Usage{InputTokens: 150, OutputTokens: 50, Cost: 0.002},
)
if err != nil {
    return err
}
```

`RecordInteraction` creates its own span — use `provider.StartSpan` when you need a shared parent span for sibling work (middleware, custom tracing).

## GenAI API

Runtime operations are available only as tracker instance methods:

- `(*Tracker).RecordInteraction`
- `(*Tracker).RecordModelInteraction`
- `(*Tracker).RecordTraceIO`
- `genai.WithScope` / `(*Tracker).RecordOperation`
- `(*Tracker).RecordTTFT`
- `(*Tracker).RecordStreamingCompletion`
- `(*Tracker).StartToolSpan`
- `(*Tracker).RecordToolResult`
- `(*Tracker).RecordAsyncFeedback`
- `(*Tracker).StartRetrievalSpan`
- `(*Tracker).RecordRetrievalResult`
- `(*Tracker).RecordEvaluations`
- `(*Tracker).RecordCacheHit`
- `(*Tracker).RecordAgentStep`

`genai.NewTrackerFromProvider` is the only supported entry point when you have a `*metry.Provider`.

```go
tracker, err := genai.NewTrackerFromProvider(provider)
```

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

## Trace context propagation (map carrier)

Pass trace context through queues by injecting into an existing message map:

```go
carrier := map[string]any{"order_id": orderID, "body": payload}
provider.InjectToMap(ctx, carrier)
// publish carrier...

consumerCtx := provider.ExtractFromMap(context.Background(), carrier)
_, end, err := provider.StartSpan(consumerCtx, "app", "consumer")
if err != nil {
    return err
}
defer end()
```

Host business keys in the map are preserved; W3C trace context and OTel baggage fields are read on extract (per the provider propagator).

## Deferred outcomes (AsyncHandle)

Capture a portable handle at enqueue time and record linked outcomes in workers without storing `SpanContext` in application code:

```go
handle, err := metry.NewAsyncHandle(ctx)
token, err := handle.Marshal()
// enqueue token...

parsed, err := metry.ParseAsyncHandle(token)
err = parsed.RecordLinkedOutcomeWithProvider(ctx, provider, "delivery.success",
    metry.TenantID("t-1"),
)
```

`genai.RecordAsyncFeedback` and `genai.RecordEvaluations` accept `metry.AsyncHandle` (portable marshal token).

For delayed GenAI feedback, capture a handle at request time and pass the same handle (or parsed token) to the worker:

```go
handle, err := metry.NewAsyncHandle(ctx)
// ... enqueue handle.Marshal() token ...

if err := tracker.RecordAsyncFeedback(ctx, parsed, 0.9, "approved"); err != nil {
    return err
}
```

`RecordAsyncFeedback` requires a valid handle (`genai.ErrInvalidAsyncHandle` otherwise). Correlation uses Span Links, not parent-child hierarchy.

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

**Footgun (GenAI scope metrics):** token/TTFT/streaming histograms require both `Provider` and `Operation` in scope or `Meta`. A scope with only `Model`/`Purpose` silently skips those metrics — set both in `genai.WithScope`.

**Footgun (GenAI errors):** `Meta.ErrorType` sets span status to Error in addition to the `error.type` attribute. `RecordToolResult(ctx, resultJSON, err)` uses the passed `err` for span status — pass `nil` on success.

**Footgun (propagation):** `Provider.InjectToMap` / `ExtractFromMap` on a nil `*Provider` are no-ops by design (safe optional wiring).

**Footgun (AsyncHandle):** marshal tokens are not signed; treat queue payloads as trusted or add application-level signing for untrusted brokers.

**BaggageMember** is a read/debug helper. Prefer typed constructors (`TenantID`, `GenAIProvider`, etc.) when writing context via `Enrich`.

**Typed baggage:** use `metry.IntAttribute`, `FloatAttribute`, and `BoolAttribute` for non-string enrich keys; `ContextHandler` restores typed slog fields when baggage carries `metry.attr.type` metadata (including after map carrier round-trip).

## GenAI scope

Declare operation metadata once per scope instead of repeating `Meta` fields:

```go
ctx = genai.WithScope(ctx, genai.Scope{
    Provider:  "openai",
    Model:     "gpt-4o-mini",
    Operation: "chat",
    Purpose:   genai.PurposeGeneration,
})
err = tracker.RecordOperation(ctx, scope, func(ctx context.Context) error {
    return tracker.RecordInteraction(ctx, genai.Meta{}, payload, usage)
})
```

## Adapter-first model interaction

When your application already has vendor-specific request/response types, implement `genai.ProviderAdapter` at the boundary and call `RecordModelInteraction`:

```go
type fakeAdapter struct{}

func (fakeAdapter) ParseRequest(req any) (genai.Payload, genai.Meta, error) {
    r, ok := req.(string)
    if !ok {
        return genai.Payload{}, genai.Meta{}, fmt.Errorf("unexpected request type")
    }
    return genai.Payload{
        InputMessages: []genai.Message{{Role: "user", Parts: []genai.ContentPart{{Type: "text", Content: r}}}},
    }, genai.Meta{Provider: "fake", Operation: "chat", RequestModel: "fake-v1"}, nil
}

func (fakeAdapter) ParseResponse(resp any) (genai.Payload, genai.Usage, error) {
    r, ok := resp.(string)
    if !ok {
        return genai.Payload{}, genai.Usage{}, fmt.Errorf("unexpected response type")
    }
    return genai.Payload{
        OutputMessages: []genai.Message{{Role: "assistant", Parts: []genai.ContentPart{{Type: "text", Content: r}}}},
    }, genai.Usage{InputTokens: 1, OutputTokens: 1}, nil
}

if err := tracker.RecordModelInteraction(ctx, fakeAdapter{}, rawReq, rawResp); err != nil {
    return err
}
```

Parse errors are returned to the caller and recorded on the span; `metry` does not ship vendor SDK adapters.

## Trace I/O mirroring

`RecordTraceIO` writes typed `genai.Payload` input/output onto span attributes using OTLP GenAI semconv keys. It respects `WithRecordPayloads` and truncation limits—the same rules as `RecordInteraction` payloads.

```go
tracker.RecordTraceIO(ctx, inputPayload, outputPayload)
```

## Sampling hints (head-based)

```go
provider, err := metry.New(
    ctx,
    metry.WithServiceName("my-ai-service"),
    metry.WithSampler(genai.NewHintSampler(metry.TraceIDRatioBased(0.1))),
)
if err != nil {
    return err
}

ctx, end, err := provider.StartSpan(ctx, "app", "request", metry.WithSpanAttributes(metry.BoolAttribute(genai.SamplingKeep, true)))
if err != nil {
    return err
}
end()
```

`gen_ai.sampling.keep=true` is evaluated at span start in the SDK sampler.
It does not depend on post-hoc status, token usage, or tail sampling.
Without keep hint, sampled parent context is inherited; the base sampler is consulted for new root spans.
Helpers that create spans internally also accept typed options, so the same hint can be passed through `StartToolSpan(...)`, `StartRetrievalSpan(...)`, `RecordAsyncFeedback(...)`, and `RecordEvaluations(...)` via `genai.WithSpanSamplingKeep()` or `genai.WithSamplingKeep()`.
When caller options contain duplicate attribute keys, helper built-in keys win.

## Retrieval spans

Use span-less callbacks — no `trace.Span`, `otel/codes`, or manual status handling in host code:

```go
retrievalCtx, end := tracker.StartRetrievalSpan(ctx, "vector.search", genai.RetrievalRequest{
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

tracker.RecordRetrievalResult(retrievalCtx, genai.RetrievalResult{
    ReturnedChunks: len(chunks),
    Distances:      distances,
})
```

## LLM evaluations

Evaluations follow the same link-based model:

```go
if err := tracker.RecordEvaluations(
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

Task14 unified-context examples (run via `make test-examples`):

| Example                     | Demonstrates                                                              |
| --------------------------- | ------------------------------------------------------------------------- |
| `examples/enrich`           | `Enrich` + `ContextHandler` slog correlation                              |
| `examples/propagation_map`  | `InjectToMap` / `ExtractFromMap` queue carrier                            |
| `examples/async_handle`     | `AsyncHandle` marshal + linked outcome                                    |
| `examples/metrics_registry` | `MetricsRegistry` histogram + counter + gauge without OTel metric imports |
| `examples/scope`            | `genai.WithScope` + worker `InjectToMap`/`ExtractFromMap` flow            |
| `examples/executor`         | Root `metry.ExecutorWrap` for generic executors                           |

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

`make test` runs tests for all modules. `make test-examples` runs the six task14 examples above.

## License

MIT
