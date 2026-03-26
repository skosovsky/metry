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

## Async feedback

```go
parent := trace.NewSpanContext(trace.SpanContextConfig{
    TraceID: traceID,
    SpanID:  spanID,
    Remote:  true,
})

if err := tracker.RecordAsyncFeedback(ctx, parent, 0.9, "approved"); err != nil {
    return err
}
```

`RecordAsyncFeedback` requires a valid parent `trace.SpanContext`; it does not generate synthetic span IDs.

## Payload truncation

Payload/tool attributes are written as strings and truncated with UTF-8-safe truncation.

- Default max length: `65536` bytes.
- Configure via `genai.WithMaxContextLength(bytes)`.
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
