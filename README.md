# metry

[![Go](https://img.shields.io/badge/Go-%3E%3D1.27.1-00ADD8?logo=go)](https://go.dev/)
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

HTTP and generic execution use root `metry.HTTPHandler` and `metry.ExecutorWrap`. gRPC is an optional integration module; core does not import gRPC. See [integration and clear-break migration](INTEGRATION.md).

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
        // Without exporters this is a noop pipeline; see production wiring below.
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
The default sampler inherits a valid parent decision and uses the ratio for roots.
`WithSampler(...)` accepts an SDK sampler as supplied and takes precedence over `WithTraceRatio(...)`.

## Production wiring

At the composition root, pass standard OTel exporters directly. For example, with
`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` installed:

```go
exporter, err := otlptracehttp.New(ctx,
    otlptracehttp.WithEndpoint("localhost:4318"),
    otlptracehttp.WithInsecure(), // local collector only
)
if err != nil {
    return err
}
provider, err := metry.New(ctx,
    metry.WithServiceName("my-service"),
    metry.WithExporter(exporter),
)
if err != nil {
    _ = exporter.Shutdown(ctx) // failed construction retains caller ownership
    return err
}
defer provider.Shutdown(ctx) // successful construction owns its exporter
```

Metric exporters use `metry.WithMetricExporter(exporter)`. To reuse an existing
SDK pipeline, use `metry.FromSDK(metry.SDKDependencies{TracerProvider: tp,
MeterProvider: mp, Propagator: propagator})`. Dependencies are borrowed:
`Shutdown` and `ForceFlush` leave them untouched; `FlushDependencies(ctx)` is an
explicit opt-in to flush them. The host closes its own SDK providers.
`Provider.SDK()` exposes the narrow composition-root bridge used by transport
adapters. Ordinary recording code uses the neutral metry APIs.

A real loopback OTLP trace/metric consumer test lives in `integration/otlp`.

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
        Usage: genai.Usage{
            InputTokens: genai.Observe(150, genai.UnitTokens, genai.SourceProvider),
            OutputTokens: genai.Observe(50, genai.UnitTokens, genai.SourceProvider),
        },
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
- `StartOperation(ctx, Operation, RecordingOptions)` followed by `OperationHandle.Finish(OperationResult)`
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

- `RedactPayloadPolicy()` redacts arbitrary JSON objects as a whole, protecting dynamic keys as well as values. Pass explicit static schema keys to preserve their structure, for example `RedactPayloadPolicy("arguments", "query")`.
- `HashPayloadPolicy()` fingerprints arbitrary JSON objects as a whole and protects content IDs/names. Fingerprints do not guarantee anonymity.
- `WithRawPayloads()` is the explicit opt-in for raw payloads.

Message roles, finish reasons and content types retain known schema enums; unknown values are sanitized too. Content IDs/names and arbitrary JSON keys are not exempt from policy. Causal IDs, prompt identities and tool call IDs use stable fingerprints by default even with content recording disabled; raw mode preserves bounded caller values. Non-raw tool names must be declared in the profile's finite tool allowlist or become `unknown`, including in the span name. Allowlisted machine names are explicitly trusted non-sensitive configuration, not a mechanism for exporting arbitrary user names.

Declared image/audio/video/file content accepts only bounded `http:`, `https:` or `urn:` references, not inline media bodies/data URIs, including in raw mode. Metry does not fetch these references. Signed media references pass payload sanitation and never leak their raw credentials in redact/hash modes. Text sanitizer input is capped before custom sanitizer work. Passing tests for these routes does not certify arbitrary host attributes or external exporter code.

The same policy applies to messages, tool arguments/results, retrieval queries, feedback text, and evaluation reasoning.

## Usage presence and provenance

See [the contract migration table](genai/MIGRATION.md) for caller changes, field names, units, boundaries and dashboard actions. `DefaultProfile().Fields` is the canonical checked catalogue; missing, incompatible or unknown declarations are rejected under the supported schema identity. Host custom attributes are not silently renamed or certified by the profile.

Metric dimensions use finite allowlists from `genai.DefaultProfile()`. Unlisted model/provider/tool/server values become `unknown` in metrics; spans retain their permitted operation metadata. Set a dimension's `Values` in a profile and pass `genai.WithProfile(profile)` at construction to permit application-specific labels. `SeriesBudget` caps combinations per instrument, including reserved overflow series. Overflow preserves token type/provenance and cost currency. Cost currency must be declared: undeclared units return a validation diagnostic instead of mixing different currencies in one counter.

In non-raw modes, machine metadata is literal only for declared safe values (including the profile's operation boundaries); undeclared operation/model/provider/server/purpose values are fingerprinted in span attributes and span names. Metric normalization still consumes the original DTO and maps unlisted dimensions to `unknown`. Retrieval source, agent/cache metadata, async names and price references follow the same checked boundary. Explicit raw mode preserves bounded caller values, but does not remove metric cardinality limits. Static allowlists are trusted local configuration: do not add dynamic URLs, user names or secrets to them.

Provider adapter parse failures return a bounded `ObservationError` with request/response field and `parse_failed` rule, without wrapping the adapter's arbitrary error text. The adapter owner handles its original errors separately. Unknown custom evaluation metric names are fingerprinted by default; the predefined metric contracts remain literal. Neither behavior defines a universal score scale.

Constructors snapshot the profile. `tracker.Profile()` returns a detached copy. The default budget is 256 series per instrument; supported budgets are 16–4096. `WithRawPayloads()` is an explicit local raw-mode permission; configuring `RawPayloadPolicy()` alone requires `profile.AllowRaw`. `tracker.CheckPolicy(identity)` resolves carried policy identity locally and cannot enable raw fallback.

`genai.CheckConformance` checks adapter declarations before running exporter-derived fixtures. It rejects ownership conflicts, unsupported profile/mapping, incompatible units and unpermitted raw mode. Its report uses bounded categorical mismatches and never includes fixture error messages or rejected payload values. Fixtures cover their controlled composition only; passing them does not certify arbitrary host/exporter code.

Adapter units bind a specific field to its canonical unit through
`FieldUnitDeclaration`; an empty declaration inherits the profile catalogue.
Every observed boundary requires `AccountingEvidence`: independently established
scenario expectations and actual exported span/measurement counts and totals.
Observation count is not series count: known zero counts once, while logical
boundaries must not produce accounting measurements. Integer instruments use
`IntegerTotal` without floating-point conversion; others use `Total`. For counters
whose exporter only supplies sums, fixtures need separate write witnesses to
prove count, including zero writes and duplicate writes. Fixtures must project
all controlled writes, actual enum values, events with their instrumentation
owner, and named JSON payloads. Missing JSON-channel evidence is rejected.
Do not copy recording inputs into the actual-evidence side of a fixture.

`Profile.Events()` declares controlled event names, instrumentation ownership,
stability and allowed fields; `Enumerations()` and `PayloadChannels()` declare
wire values and input/encoding/budget boundaries. Returned declarations are
detached copies. Canonical boundary names, operations and accounting semantics
cannot be reinterpreted under the supported profile identity. Conformance also
rejects observations whose boundary has no declared adapter owner. The synthetic
two-adapter fixtures exercise sync, streaming, cancellation and detached resume.

`Usage` uses typed `Measurement[int]` and `Measurement[float64]` fields. A zero-value measurement is absent. `Observe(0, UnitTokens, SourceMeasured)` is a known zero and creates a zero histogram observation. Sources are `SourceMeasured`, `SourceProvider`, and `SourceEstimated`; an estimate stays distinguishable in both spans and metrics. `SourceUnknown` is valid only for an absent measurement.

Input/output totals are inclusive. Cache/reasoning components are subsets, not additional billable tokens; overlapping components are not summed. Unknown totals are never reconstructed from an incomplete breakdown. Cost requires an explicit caller-owned currency code (including bounded uppercase custom units); metry does not guess prices or currencies.

Negative or non-finite measurements, incompatible units, unsupported mapping and inconsistent components return an `ObservationError` wrapping `ErrInvalidObservation` before operation/interaction spans or metrics are written. Errors contain a bounded field/rule, never the rejected value. Scalar `Usage` literals are removed; callers must supply presence, unit and provenance explicitly.

Caller adapters must normalize exhaustive, disjoint exclusive buckets to inclusive totals exactly once before recording. Set `Usage.Mapping` to `"inclusive"` after normalization. Core does not recognize backend vocabularies or sum components. If any exclusive bucket is missing, leave the total absent; do not turn partial knowledge into a measured total. Preserve per-measurement provenance and check integer overflow in the adapter. The synthetic mapping contract fixtures cover complete and partial breakdowns and reject reapplying their exclusive mapping to inclusive input.

## Execution intervals, causal identity and streaming

Call `Runtime.StartOperation` before I/O and `handle.Finish` after I/O. It records a real execution interval and uses a local monotonic clock for duration. `Finish` rejects a supplied `OperationResult.Duration`: the handle measures its own interval. `RecordOperation` and `RecordInteraction` are completed-observation spans, marked `metry.observation.kind=completed`; their span timestamps describe recording, not the earlier execution. Their caller-supplied durations must not be inferred by subtracting wall clocks on different workers.

An effective operation name is required. Completed recording resolves an omitted
name from caller-declared `Scope.Operation`; if neither is present, or the name
is blank, invalid UTF-8 or over the bound, it returns a diagnostic before writing
spans or metrics. A model-only or purpose-only scope is not a substitute for an
operation name. Standalone final streaming completion and noop recording validate
the same effective-name contract before committing any measurements.

`RecordingOptions.Identity` is optional. In causal mode the caller supplies run/operation IDs and a kind; physical attempts also require an attempt ID. Retry keeps operation identity and changes attempt identity. `IdentityLogical` records a summary without increasing resource or duration metrics; `IdentityOperation` represents a standalone physical operation and `IdentityAttempt` an explicit physical attempt. IDs are snapshotted, bounded and span-only, never metric labels. Metry does not generate business IDs, schedule retries, prove global uniqueness or deduplicate across processes.

An active context can supply the parent. With `Detached: true`, recording creates a new root linked to the context's trace reference, including a restored snapshot after the predecessor has ended. Additional `Links` use existing opaque `AsyncHandle` references for fan-in; duplicates are removed. Business identity remains separate from trace tokens. `Predecessor` is caller-owned evidence (or unknown), not an inference from the sampling flag. A carried `PolicyID` is resolved locally before recording; an unknown identity is rejected, never replaced with raw mode.

For streaming, `RecordStreamingUpdate` validates intermediate cumulative usage without recording or storing totals. Only successful `Finish` commits final usage and cost. An aborted stream with missing final usage stays missing even if earlier updates contained counts. `OperationResult.Streaming` supplies TTFT measured from the same local start; `FirstTokenPresent` distinguishes unknown TTFT from a known zero. Impossible intervals are rejected before finalization and may be corrected. Concurrent successful finalization records once; further calls return `ErrFinalized`.

`RecordStreamingCompletion` accepts a standalone final DTO with explicit usage and total duration. It is a stateless completed observation, not a deduplication handle: the producer must deliver it once. Use `StartOperation`/`Finish` when local duplicate-finalization protection is needed. Do not separately call `RecordTTFT` for a final DTO that already supplies TTFT, or emit the same usage through both recording paths.

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

Metric names and label values must be static or bounded by the host. The generic
registry does not inherit the GenAI series budget.

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

`RecordRetrievalResult` returns a diagnostic for negative chunk counts, more than 4,096 distance values or non-finite distances before copying or mutating span attributes. Handle this telemetry diagnostic separately from the retrieval's business result/error. Finite distances may be negative; their scale belongs to the caller's metric contract.

## Completed evaluation observations

Create an app-scoped evaluation recorder from the tracker. It shares the provider, profile and payload policy but does not extend the execution runtime or run a grader:

```go
score := 0.91
recorder := adapterTracker.EvaluationRecorder()
if err := recorder.Record(ctx, genai.Evaluation{
    ObservationID: "evaluation-42",
    Metric:        genai.EvaluationFaithfulness,
    Status:        genai.EvaluationSucceeded,
    Score:         &score,
    Reasoning:     "Grounded in retrieved chunks.",
    TraceReference: &parsed, // optional; nil creates a standalone root
    Association: genai.EvaluationAssociation{
        ExperimentID:     "experiment",
        DatasetReference: "dataset-content-digest",
        CaseID:           "case-1",
        TrialID:          "trial-2",
        TargetReference:  "target-content-digest",
        GraderReference:  "grader-content-digest",
    },
    ArtifactReference: "opaque-artifact-identity",
}); err != nil {
    return err
}
```

`metry` does not provide trace mutation / PATCH APIs for updating finished spans—use Span Links for post-hoc feedback and evaluations.

`ObservationID`, a static machine-readable `Metric`, and `Status` are required. A successful result has exactly one of finite `Score` (including zero; no universal range) or bounded `Outcome`. Failed/skipped results have neither, so they cannot silently become score zero. All six association fields are required when any experiment association field is supplied; otherwise association is optional. Dataset/target/grader references are opaque caller identities or content digests, not resources fetched by the library.

Recording always creates a new root, optionally linked to a valid trace reference; an invalid supplied reference is rejected before export. The target need not have been sampled/exported. The recorder obeys normal sampling and never forces export. It does not infer execution absence, store artifacts or deduplicate deliveries. Repeated deliveries retain their observation identity, while trial/content identities distinguish changed inputs.

The wire representation of IDs, outcomes and opaque references is a stable fingerprint in non-raw modes, retaining equality without exposing values; hashes are not anonymity. Explicit `WithRawPayloads()` preserves the supplied values. Reasoning is exported only when payload recording is enabled and passes the same sanitizer plus the event byte limit. Machine metric names must be static non-sensitive schema names. Custom linked attributes from the removed `RecordEvaluations` API are not carried forward: supply business association explicitly. `NoopEvaluationRecorder()` performs validation but drops telemetry.

## Payload truncation

Structured payload/tool attributes are JSON strings. Sanitization precedes encoding. Values that fit retain their shape; oversized values become a valid envelope with `truncated: true` and a retained prefix of top-level elements/fields when it fits. If no data fits, the envelope is just `{"truncated":true}`. Serialized JSON is never sliced. Plain-text tool inputs are encoded as JSON strings, not emitted as malformed JSON fragments.

- Default max length: `65536` bytes.
- Configure via `genai.WithMaxContextLength(bytes)`.
- The minimum context budget is 18 bytes, sufficient for the marker-only envelope. Smaller/zero/negative configured budgets return a bounded configuration diagnostic; they do not silently restore the default.
- Typed message payloads are limited to 4,096 messages per direction, 4,096 content parts across directions, 1 MiB aggregate input text/JSON, and JSON depth 32 before sanitizer work. Structured serialization also has a 1 MiB input cap. Tool JSON receives the same byte/depth checks before sanitization.
- Span **event** text (e.g. LLM-judge reasoning on evaluation events when payloads are enabled) uses a separate, smaller default (`4096` bytes) via `genai.WithMaxEventLength`, because backends often cap event attributes more strictly than span attributes.
- Text-only feedback/query/reasoning channels use UTF-8-safe text truncation, not a JSON envelope.

Invalid/beyond-bound message payloads return `ObservationError` before operation/interaction writes. `RecordTraceIO` now returns an error and leaves the active span untouched on rejection. Invalid handle finalization can be corrected without consuming the finalization guard. Tool payload rejection omits the payload and records a bounded `metry.observation.rejected` event with field/rule; it never changes the tool's business outcome. Custom attributes are outside this checked payload boundary.

## HTTP middleware

```go
import "github.com/skosovsky/metry"

handler := metry.HTTPHandler(provider, next, "http.request")
```

## Generic execution telemetry

Use `metry.ExecutorWrap` to instrument any function with shape `func(context.Context, Req) (Res, error)` (database calls, LLM clients, internal pipelines) without tying it to HTTP or gRPC.

**Logging:** structured logs (start, errors, panics) are emitted only when you pass `slog` via `metry.WithExecutorLogger`. Without a logger you still get **traces and metrics** only. Start and error/panic lines include `trace_id` when the span has a W3C trace id. **Request/response bodies are never logged** on purpose (PII / payload safety); this is library policy, not an omission.

**Options:** `WithExecutorLogStart` / `WithExecutorLogError` map to optional telemetry in the task spec; there is no response-body logging (`LogResponses`) unless introduced in a separate change.

```go
import "github.com/skosovsky/metry"

run := metry.ExecutorWrap(provider, "my.pipeline.step", func(ctx context.Context, req MyReq) (MyRes, error) {
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

wrapped := metry.ExecutorWrap(provider, "my_op", myLogic)
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
| `examples/genai_standalone` | Model/tool/retrieval without causal IDs, experiments or execution store |

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
