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
        // metry.WithSampler(genai.NewHintSampler(sdktrace.TraceIDRatioBased(0.1))),
        // metry.WithExporter(...),
        // metry.WithMetricExporter(...),
    )
    if err != nil {
        return nil, nil, err
    }

    tracker, err := genai.NewTracker(
        provider.MeterProvider.Meter("metry/genai"),
        provider.TracerProvider.Tracer("metry/genai"),
        genai.WithRecordPayloads(true),
    )
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

_, span := provider.TracerProvider.Tracer("app/handler").Start(ctx, "request")
tracker.RecordInteraction(ctx, span,
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
span.End()
```

## GenAI API

Runtime operations are available only as tracker instance methods:

- `(*Tracker).RecordInteraction`
- `(*Tracker).RecordTTFT`
- `(*Tracker).RecordStreamingCompletion`
- `(*Tracker).StartToolSpan`
- `(*Tracker).RecordToolResult`
- `(*Tracker).RecordAsyncFeedback`
- `(*Tracker).StartRetrievalSpan`
- `(*Tracker).RecordRetrievalResult`
- `(*Tracker).RecordEvaluations`

Stateless helpers remain package-level:

- `genai.RecordCacheHit`
- `genai.RecordAgentStep`

`genai.NewTracker` requires both `meter` and `tracer` explicitly.
Use `go.opentelemetry.io/otel/metric/noop` for traces-only setups.
If you need traces only, pass a noop meter provider:

```go
import noopmetric "go.opentelemetry.io/otel/metric/noop"

noopMeter := noopmetric.NewMeterProvider().Meter("metry/genai")
tracker, err := genai.NewTracker(noopMeter, provider.TracerProvider.Tracer("metry/genai"))
```

## Sampling hints (head-based)

Snippet note: examples in this section assume imports for `go.opentelemetry.io/otel/trace` and `go.opentelemetry.io/otel/sdk/trace`.

```go
provider, err := metry.New(
    ctx,
    metry.WithServiceName("my-ai-service"),
    metry.WithSampler(genai.NewHintSampler(sdktrace.TraceIDRatioBased(0.1))),
)
if err != nil {
    return err
}

_, span := provider.TracerProvider.Tracer("app").Start(
    ctx,
    "request",
    trace.WithAttributes(genai.SamplingKeepKey.Bool(true)),
)
span.End()
```

`gen_ai.sampling.keep=true` is evaluated at span start in the SDK sampler.
It does not depend on post-hoc status, token usage, or tail sampling.
Without keep hint, sampled parent context is inherited; the base sampler is consulted for new root spans.
Helpers that create spans internally also accept `trace.SpanStartOption`, so the same hint can be passed through `StartToolSpan(...)`, `StartRetrievalSpan(...)`, `RecordAsyncFeedback(...)`, and `RecordEvaluations(...)`.
When caller options contain duplicate attribute keys, helper built-in keys win.

## Retrieval spans

Snippet note: this example also assumes import of `go.opentelemetry.io/otel/codes`.

```go
retrievalCtx, retrievalSpan := tracker.StartRetrievalSpan(ctx, "vector.search", genai.RetrievalRequest{
    Provider: "qdrant",
    Source:   "knowledge_base",
    Query:    query,
    TopK:     5,
}, trace.WithAttributes(genai.SamplingKeepKey.Bool(true)))
defer retrievalSpan.End()

chunks, distances, err := retriever.Search(retrievalCtx, query)
if err != nil {
    retrievalSpan.RecordError(err)
    retrievalSpan.SetStatus(codes.Error, "retrieval failed")
    return err
}

tracker.RecordRetrievalResult(retrievalSpan, genai.RetrievalResult{
    ReturnedChunks: len(chunks),
    Distances:      distances,
})
```

## Async feedback

```go
parent := trace.NewSpanContext(trace.SpanContextConfig{
    TraceID: traceID,
    SpanID:  spanID,
    Remote:  true,
})

if err := tracker.RecordAsyncFeedback(
    ctx,
    parent,
    0.9,
    "approved",
    trace.WithAttributes(genai.SamplingKeepKey.Bool(true)),
); err != nil {
    return err
}
```

`RecordAsyncFeedback` requires a valid parent `trace.SpanContext`; it does not generate synthetic span IDs.

## LLM evaluations

```go
if err := tracker.RecordEvaluations(
    ctx,
    parent,
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
    trace.WithAttributes(genai.SamplingKeepKey.Bool(true)),
); err != nil {
    return err
}
```

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

`make test` runs tests for all modules.

## License

MIT
