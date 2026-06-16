# task14 migration (Unified Observability Context)

Canonical copy: [`.cursor/docs/task14-migration.md`](.cursor/docs/task14-migration.md) — kept in sync with this file via `check-task14.sh`.

## Phase 1: Enrich replaces raw baggage

**Removed:** `SetBaggageValue`, `BaggageValue`

**Use instead:**
```go
ctx = metry.Enrich(ctx, metry.TenantID("t-1"), metry.PatientID("p-9"))
logger := slog.New(metry.ContextHandler{Handler: baseHandler})
```

Empty semantic IDs (`TenantID("")`, etc.) are rejected.

## Phase 2: Map carrier replaces JSON propagation

**Removed:** `propagation.InjectToJSON`, `ExtractFromJSON`, `*WithPropagator` JSON helpers

**Use instead:**
```go
carrier := map[string]any{"business_key": "value"}
provider.InjectToMap(ctx, carrier)
consumerCtx := provider.ExtractFromMap(context.Background(), carrier)
```

Low-level package API: `propagation.InjectToMap(ctx, propagator, carrier)`.

Nil `*Provider` on `InjectToMap` / `ExtractFromMap` is a no-op by design (safe optional wiring).

## Phase 3: AsyncHandle replaces SpanContext in genai async APIs

**Changed:** `RecordAsyncFeedback` and `RecordEvaluations` accept `metry.AsyncHandle`. Error: `genai.ErrInvalidAsyncHandle` (alias of `metry.ErrInvalidAsyncHandle`).

**Removed from public metry API:** `NewAsyncHandleFromSpanContext`, `(AsyncHandle).SpanContext()`, `(AsyncHandle).Async()`, `CaptureAsyncHandle`, `RecordLinkedOutcome(..., trace.Tracer, ...)`, **`metry.StartLinkedSpan`**.

**Span-less GenAI (round 4+):** `RecordInteraction`, `RecordModelInteraction`, `StartToolSpan`, `StartRetrievalSpan`, and related helpers create or use spans from `context` only. Sampling hints use `genai.WithSamplingKeep()` / `genai.WithSpanSamplingKeep()` with `genai.ChildSpanOption` — not raw `trace.SpanStartOption`.

**Use instead:**
```go
handle, _ := metry.NewAsyncHandle(ctx)
token, _ := handle.Marshal() // queue payload
// worker:
parsed, _ := metry.ParseAsyncHandle(token)
tracker.RecordAsyncFeedback(ctx, parsed, score, text)
```

`RecordLinkedOutcomeWithProvider(ctx, provider, spanName, attrs...)` uses tracer name `metry.async` by default.

**Security note:** handle tokens are not signed; treat queue payloads as trusted or add application-level signing for untrusted brokers.

## Phase 4: GenAI Scope

**New:** `genai.WithScope`, `(*Tracker).WithScope`, `(*Tracker).RecordOperation` (creates `genai.operation` span), automatic `Meta` defaults in `RecordInteraction`.

Host applications must use `Enrich` / `WithScope` only — do not rely on internal `context.WithValue` keys.

## Phase 5: MetricsRegistry

**New:** `metry.NewMetricsRegistry`, `NewHistogram`, `NewCounter`, `NewGauge`, `Labels` — host apps should not import `go.opentelemetry.io/otel/metric` for domain metrics.

**API:**
- `NewHistogram(name, buckets) (HistogramMetric, error)` — `Record(ctx, float64, Labels)`
- `NewCounter(name) (CounterMetric, error)` — `Add(ctx, int64, Labels)`
- `NewGauge(name) (GaugeMetric, error)` — `Record(ctx, float64, Labels)`

**Duplicate names:** the same metric name cannot be registered twice across instrument types (`ErrDuplicateMetric`).

**Footgun:** zero-value metric wrappers silently no-op — check `OK()` after `New*`.

**Footgun (labels):** empty keys and empty values are skipped in `LabelsOf`, `copyLabels`, and `MetricsRegistry` attribute conversion.

**Footgun (GenAI scope metrics):** GenAI token/operation histograms (`RecordInteraction`, `RecordTTFT`, etc.) require both `Provider` and `Operation` in scope or `Meta`. Scope with only `Model`/`Purpose` silently skips those metrics.

## Phase 6: Round 5 (OTel encapsulation)

**New:** opaque `metry.TraceSampler`, `metry.SpanExporter`, `metry.MetricExporter` — `WithSampler` / `WithExporter` / `WithMetricExporter` accept these types. Helpers: `metry.NeverSample()`, `metry.AlwaysSample()`, `metry.TraceIDRatioBased(ratio)`. `genai.NewHintSampler` returns `metry.TraceSampler`.

**New:** `(AsyncHandle).RecordLinkedSpan(ctx, provider, name, fn)` — callback API with `LinkedSpanWriter` for linked async spans.

**Removed:** `genai.NewTracker` / `genai.NewTrackerWithTracer` — use `genai.NewTrackerFromProvider(provider)` only.

**Security:** `security.RecordSecurityEventWithProvider(ctx, provider, ...)` only — creates a span when ctx has no active span.

**Baggage:** non-string attributes via `metry.FloatAttribute`, `metry.BoolAttribute`, and `metry.IntAttribute` — typed restore via baggage metadata.

## Phase 7: Round 6 (strict host boundary)

**Removed:** all public `genai.*OnSpan` methods; public `traceutil/` package; `Provider.OtelProvider()`, `Provider.Tracer()`, `Provider.Meter()`; `security.RecordSecurityEvent` (silent no-op).

**Renamed:** `metry.SpanStartOption` → `metry.StartSpanOption`; `genai.SpanStartOption` → `genai.ChildSpanOption`.

**New root middleware:** `metry.HTTPHandler`, `metry.GRPCServerStatsHandler`, `metry.GRPCClientStatsHandler`, `metry.GRPCServerOptions`, `metry.GRPCClientDialOption`, `metry.ExecutorWrap`. Subpackages `middleware/http`, `middleware/grpc`, `middleware/executor` are thin aliases.

**GenAI wiring:** internal `genaiwire` (init from `metry`) supplies meter/tracer for `genai.NewTrackerFromProvider` — no public OTel types on the host boundary.

**Linked outcome:** `RecordLinkedOutcomeWithProvider` preserves typed attributes (`FloatAttribute`, `BoolAttribute`) on spans.

**Scope baggage:** `metry.GenAIProvider`, `GenAIModel`, `GenAIOperation`, `GenAIPurpose` semantic constructors.

**BaggageMember:** read/debug API for baggage values on context. Prefer typed constructors (`TenantID`, `GenAIProvider`, etc.) for writes via `Enrich` / `WithScope`.

**StringAttribute:** use for application-specific keys only. Host apps should prefer semantic constructors (`TenantID`, `FloatAttribute`, GenAI baggage keys) for observability context.

**ErrorType:** when `genai.Meta.ErrorType` is set, `RecordInteraction` writes the `error.type` attribute and sets span status to Error.

**RecordToolResult:** signature is `RecordToolResult(ctx, resultJSON string, err error)`. Pass a non-nil `err` to mark tool failure on the span; `nil` marks success.

## Implementation notes (plan vs code)

The task14 implementation plan described an unexported `context.WithValue` key for enrich slog attributes and a separate scope context key. The shipped design uses **baggage-only** propagation:

- **Enrich → slog:** `ContextHandler` reads typed attributes from OTel baggage members that carry `metry.attr.type` metadata (not all baggage keys).
- **GenAI scope:** `genai.WithScope` writes scope fields via `metry.Enrich` / baggage keys (`metry.genai.*`), not a private context key.
- **Int attributes:** use `metry.IntAttribute` — supported in Enrich, span attributes, map carrier round-trip, and typed slog output.

Reassign `ctx = metry.Enrich(...)` on every call when accumulating baggage across multiple enrich steps.

## Phase 8: Round 7 (span status symmetry + linked guards)

**SpanOK:** `RecordOperation`, `RunLinkedSpan`, and `RecordInteraction` (when `ErrorType` is empty) set span status to Ok via `internal/traceutil.SpanOK`.

**MutateRecordingSpan:** linked-span attribute/event mutations in `AsyncHandle.RecordLinkedSpan` and `RecordLinkedOutcome` skip work when the span is not recording.

**Scope dedup:** `RecordInteraction` reads GenAI scope from baggage once per call (`mergeMetaFromScopeWithScope`).

**E2E:** async feedback and evaluations flows use `InjectToMap` → worker `ExtractFromMap`; scope E2E asserts `OperationPurpose` on interaction spans.

## Phase 9: Round 8 (100% closure)

**SpanOKIfUnset:** `Provider.StartSpan`, `StartToolSpan`, and `StartRetrievalSpan` end callbacks set Ok when span status is still Unset.

**RecordRetrievalResult:** sets span Ok on success (mirror `RecordToolResult`).

**Security:** `AddEvent` guarded by `MutateRecordingSpan`; standalone `security.intervention` span Ok via `EndSpanOKIfUnset`.

**Scope dedup:** `recordUsageMetrics` and `recordOperationDuration` no longer re-read scope (meta is pre-merged in `RecordInteraction`).

**Labels:** empty string values skipped in `MetricsRegistry`.

**E2E:** all async outcome and interaction spans assert Ok status via `AssertSpanStubOkStatus`.

## Phase 10: Round 9 (100% closure)

**MetricsRegistry:** `NewCounter(name) (CounterMetric, error)` with `Add(ctx, int64, Labels)`; `NewGauge(name) (GaugeMetric, error)` with `Record(ctx, float64, Labels)`. Unified `names` map prevents cross-type duplicate metric names (`ErrDuplicateMetric`).

**Labels symmetry:** `LabelsOf` and `copyLabels` skip empty keys and empty values (same as `labelsToAttributes`).

**Enrich guard:** `enrichSpan` uses `MutateRecordingSpan` instead of direct `IsRecording` + `SetAttributes`.

**Unit SpanOK:** `RecordLinkedOutcomeWithProvider` tests and security ActiveSpan parent assert Ok status.

**GenAI metrics:** `TestRecordStreamingCompletion_WithScope_UsesScopeProviderInMetrics` mirrors TTFT scope regression.

**Examples:** `examples/scope` demonstrates producer `InjectToMap` → worker `ExtractFromMap`; `examples/metrics_registry` demonstrates histogram + counter + gauge in worker context.

## Phase 11: Round 10 (100% closure)

**Gauge parity:** E2E `task14_metrics_e2e_test.go` and `examples/metrics_registry` exercise `NewGauge("queue_depth")` alongside histogram + counter in worker context.

**Labels test matrix:** `TestCopyLabels_SkipsEmptyKeyAndValue` completes 3/3 pipeline coverage (`LabelsOf`, `copyLabels`, `labelsToAttributes`).

**Enrich guard test:** `TestEnrich_WithoutActiveSpan_NoSpanExport` — baggage without span export.

**Docs:** task14.md Phase 5 lists Histogram + Counter + Gauge; task2.md prose fixed; README footgun without internal symbols.

**Gates:** G28–G32.

## Phase 12: Round 11 (100% test infra closure)

**E2E metrics:** `task14_metrics_e2e_test.go` asserts histogram sum, counter value, gauge value, and label attrs via `metrytest.CollectResourceMetrics` + `Int64SumValue` / `GaugeFloat64Value` / `FirstInt64SumAttr` / `FirstGaugeAttr`.

**metrytest:** `FindInt64Sum`, `Int64SumValue`, `FirstInt64SumAttr`, `FindGauge`, `GaugeFloat64Value`, `FirstGaugeAttr` — parity with existing histogram/sum helpers.

**Docs:** migration L3 canonical link; README labels footgun mirrors migration Phase 5.

**GenAI design note:** span-start `SetAttributes` on freshly created spans in `genai/*` is intentional (direct span reference, no context re-lookup); post-start mutations use `mutateSpan` / `MutateRecordingSpan`.

**Gates:** G33–G35.
