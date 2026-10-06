# Instrumentation contract migration

This is a clear break. There are no scalar-usage wrappers or legacy evaluation
entry points. Host adapters must translate their own types at the composition
root; metry does not own execution, retry decisions, storage or pricing.

Generic `Provider.StartSpan` completion now preserves the explicitly assigned
OTel status and leaves unknown outcomes Unset. Calling its returned `SpanEnd`
does not establish success. Hosts that know a successful result may explicitly
set Ok through their OTel span; error status remains intact through public-only
span wrappers. This does not change explicit outcome owners in instrumentation
such as executor callbacks or GenAI operation handles.

`RuntimeFromProvider` has been removed. Replace it with
`NewRecorderFromProvider` and handle the returned error. For intentionally disabled
telemetry, select `NoopRecorder` at the host composition root. Invalid budgets,
profile/policy mismatches and SDK instrument creation failures never select noop
implicitly.

Durable `ContextWithTraceSnapshot` decodes its W3C reference independently of
network propagators, including baggage-only/custom transports. The provider's
remote-trace policy governs restoration: rejection returns
`ErrTraceSnapshotRejected` and the original context. Success replaces any foreign
trace reference with the captured reference while retaining the caller's baggage
and local enrichment. Snapshot tokens themselves never carry baggage.

`Enrich` now accepts raw strings consistently; callers must stop pre-encoding
values. Literal `%20` stays literal and transport encoding occurs once. Allowlisted
ordinary W3C baggage without a private type marker is imported as string;
invalid explicit markers and reserved collisions remain rejected. Visible/egress
baggage uses a deterministic sorted fitting prefix within pinned SDK limits.
Overflow no longer removes trusted values from private span/log context.

Both package and Tracker `WithScope` now store original defaults privately.
They do not enrich the parent span, ContextHandler logs or transport baggage.
Explicit Meta fields override defaults; private local defaults override admitted
enrichment fallback. Repeated partial scope writes merge non-empty local fields.
Each tracker applies its own metadata policy when recording, so safe-label and
raw permissions belong to the emitter rather than the scope creator. For
intentional raw host enrichment use `metry.Enrich` explicitly. Durable snapshot
and network baggage do not serialize private defaults; carry/reapply them through
caller-owned state when a worker needs them.

| Previous contract | Current contract | Names / units / boundary | Consumer action |
|---|---|---|---|
| Scalar `Usage`, zero treated as missing | Typed `Measurement[int]` / `Measurement[float64]` with explicit presence/source/unit | Existing total/component attributes remain; each present measurement adds `metry.gen_ai.measurement.<field>.source` and `.unit` | Replace literals with `Observe`; use zero-value measurement for absence and present zero for a known zero |
| Implicit provenance | Per-measurement measured/provider-reported/estimated provenance | Span source fields and `metry.usage.source` metric label distinguish sources | Update dashboards to group/filter provenance; do not treat estimates as measured |
| Inclusive totals mixed with subsets | Totals stay inclusive; overlapping subsets are separate | `gen_ai.client.inference.operation.input_tokens` / `output_tokens` vs `metry.gen_ai.client.token.component.usage`, unit `{token}` | Remove totals-plus-components dashboard sums; normalize exhaustive exclusive buckets once in the host adapter; incomplete buckets leave total absent |
| Implicit cost currency | Explicit declared currency and independently sourced cost | `metry.gen_ai.cost`, `metry.gen_ai.cost.currency`, explicit currency dimension; no conversion | Declare currencies in the profile; group currencies separately; host retains prices, conversion and quotas |
| Recording a completed result treated as an execution span | `StartOperation` before I/O and local `Finish` after I/O | Execution duration uses local monotonic time; completed helpers mark `metry.observation.kind=completed` | Choose one owner for each boundary; do not derive execution latency from completed-observation timestamps |
| Logical totals added again after retries | Physical operations/attempts account resources; logical summaries do not | Causal identity remains span-only; failed physical retries still count | Supply stable operation IDs and distinct attempt IDs; remove summary accounting from host adapters |
| Scalar streaming output count and separately repeated TTFT/usage | Final DTO uses `Usage`; local handle commits once | TTFT/total duration measured from the same start; `FirstTokenPresent` distinguishes zero/unknown | Intermediate updates must not increment totals; do not record the same final usage or TTFT through two paths |
| Mandatory handle and numeric-only `RecordEvaluations` | Separate `EvaluationRecorder.Record` with status, score xor category, optional trace and experiment association | New `evaluation` completed-result root (extension), optional link; failed/skipped contain no score | Remove old calls; supply observation ID, ready result and optional full experiment/case/trial/content references; keep artifacts outside metry |
| Raw names, URLs and IDs accepted as span metadata | Declared safe machine labels or bounded fingerprints in non-raw modes | Attributes and span names follow local policy; metric unknown/overflow buckets remain bounded | Declare static non-sensitive labels, update queries for wire fingerprints; do not allowlist dynamic URLs/user IDs |
| Arbitrary JSON keys survived redact/hash | Whole-object sanitation unless static schema keys are explicitly allowlisted | Messages/tool args/results are valid bounded JSON; hash is not anonymity | Review key allowlists, remove assumptions about unconditionally preserved object shape; raw export requires explicit opt-in |
| Serialized JSON sliced as text | Array channels retain complete elements with separate marker; arbitrary tool channels may use an envelope | Minimum context budget remains 18 bytes; text channels retain text truncation | Read channel-specific truncation evidence; never repair sliced JSON |
| Unbounded sanitizer inputs or inline media bodies | Input byte/depth/count limits; media references only, no fetching | Limits precede sanitizer work; even raw mode rejects inline declared media bodies | Keep binary content/artifacts in host storage; submit bounded opaque references |
| Arbitrary adapter error messages wrapped into diagnostics | Bounded request/response `parse_failed` diagnostic | Original adapter errors are not exported or wrapped | Handle original errors within the host adapter; do not depend on `errors.Is` matching arbitrary adapter errors |
| Retrieval output attributes written without validation | `RetrievalHandle.Finish` returns an error before invalid writes | Finite optional relevance; bounded document array; nonnegative count; no imposed score scale | Handle telemetry diagnostics separately from business results; check returned errors |
| Completed observation or standalone streaming completion without operation name silently dropped usage metrics | Effective operation name required, resolved from caller scope when omitted | Required `gen_ai.operation.name`; malformed name rejected before span/metric writes, including noop paths | Supply an operation name or `Scope.Operation`; model/purpose alone is insufficient |

## Core helpers, async and lifecycle clear break

`metry.DoctorID` and `metry.PatientID` are removed; declare domain helpers in the
consumer using `StringAttribute`. Generic `TenantID` and `SubjectID` remain.
`NoopAsyncHandle` now has an explicit private disabled state with no trace IDs.
`IsValid` is false; `Marshal` returns `ErrInvalidAsyncHandle` and an empty token.
Root linked recording and GenAI async-result/feedback recording drop disabled handles,
including with an enabled provider. Causal `Links` omit disabled references;
`Evaluation.TraceReference` rejects them because it must reference a real origin.
Zero invalid handles still report errors. Persist caller business state without
inventing a trace token when telemetry is disabled. Discard previously persisted
synthetic noop tokens; capture an actual observed origin when a real link is needed.
Parsing validates token syntax and limits, not proof of origin; the host owns
reference authenticity.

`LinkedSpanWriter` supports concurrent callback workers that the caller joins
before return. Its copies share synchronized state; first non-nil `SetError`
wins, nil does nothing, callback return error takes precedence. After callback
return or panic, writer operations are inert. Zero writers safely drop calls.
Callbacks must not own business work: disabled handles do not run them.

Host payload policies and tool-error classifiers retain their panic values for
the host. A panic during interval `Finish` now finalizes telemetry as error with
bounded `unknown` error class and a categorical callback-failure event, without
raw panic text even under raw diagnostic opt-in. The handle becomes terminal;
later `Finish` or `Close` returns `ErrFinalized`. Ordinary validation errors stay
correctable. Callbacks in start/completed-result paths, including async feedback
sanitization, run before a new span is created. `RecordTraceIO` does not own the
borrowed span and never ends it on callback failure. Diagnostic-policy sanitizer
failures retain their existing guarded, fail-closed behavior.

Provider owns configuration, lazy instruments and shutdown state. Owned shutdown
is terminal/idempotent; concurrent callers await the same first result even on
failure. The first cancelled context remains terminal. Use a fresh live shutdown
context, bounded by the ten-second limit and any earlier caller deadline.
Borrowed SDK dependencies retain host-owned shutdown/flush lifetime.

## Recorder and byte-budget API clear break

`Runtime` is renamed to `Recorder`, `Tracker.Runtime()` to `Tracker.Recorder()`,
`NewRuntimeFromProvider` to `NewRecorderFromProvider`, and `NoopRuntime` to
`NoopRecorder`. These names have no aliases. The init-time `MeterTracer(any)`
bridge and its obsolete `ErrMeterRequired`, `ErrTracerRequired`,
`metry.ErrNilMeterProvider` and `metry.ErrInvalidProviderType` errors are removed;
SDK dependency validation belongs to `metry.FromSDK`. The recorder observes caller work;
it does not execute, retry or schedule it. Consumer-owned narrow interfaces remain
supported. Constructing from a nil provider is an error; a zero provider exposes
noop SDK dependencies through the typed `Provider.SDK()` contract.

`OperationHandle.RecordStreamingUpdate` becomes `ValidateStreamingUpdate(Usage)`;
it checks intermediate observations without exporting or retaining usage.
`Tracker.RecordTTFT` and `Runtime.RecordTTFT` are removed. Final
`OperationResult.Streaming` / `StreamingCompletion` with `FirstTokenPresent`
are the sole TTFT sources, including a known zero. Successful handle Finish
records once; a standalone completed DTO must be delivered once by its owner.

`WithMaxContextLength` becomes `WithPayloadByteLimit(int)`;
`WithMaxEventLength` becomes `WithEventTextByteLimit(int)`. Effective contract
getters use `PayloadByteLimit()` and `EventTextByteLimit()`; channel
`BudgetSetting` values become `payload_byte_limit` and `event_text_byte_limit`.
Limits remain bytes with identical ranges and validation. Telemetry attribute
and metric names, schema ID, instrumentation scope version and schema URL do not
change: these are API/declaration renames with unchanged wire semantics.

## Outcome and handle clear break

The following signatures are removed, with no compatibility wrappers:

- `Runtime.StartTool(ctx, call, opts...) (context.Context, ToolEnd)` and `Runtime.RecordToolResult(ctx, result)` become `Recorder.StartTool(ctx, call, opts...) (context.Context, *ToolHandle, error)` and `ToolHandle.Finish(result) error`.
- `Tracker.StartRetrievalSpan(ctx, name, request, opts...)` and `Tracker.RecordRetrievalResult(ctx, result)` become `Tracker.StartRetrieval(ctx, name, request, opts...) (context.Context, *RetrievalHandle, error)` and `RetrievalHandle.Finish(result) error`. Recorder exposes the same start method.
- Adapter `ParseResponse(any) (Payload, Usage, error)` becomes `ParseResponse(any) (ResponseObservation, error)`. Return all observed response facts from that call: `Payload`, `Usage`, `Metadata: ResponseMetadata{Model: actualModel}`, `Status` and `ErrorType`. Domain request/response types require no metry interface, and adapters require no mutable request state.

All operation, tool and retrieval handles expose `Close() error`. Start before host I/O; finish after I/O. Defer `Close` to cover exits that cannot provide a valid result. A successful finish already closes the handle, so a deferred close returns `ErrFinalized`; it produces no additional writes. Invalid finish is repairable and produces no final usage or outcome writes. Close commits unknown without fabricated usage, tool result or retrieval documents. Live durations use a local monotonic clock; completed observations retain their supplied known duration and completed marker.

`Meta.Status`, `OperationResult.Status`, `ToolResult.Status` and `RetrievalResult.Status` use `Outcome`. Empty and `OperationStatusUnset` are unknown, `OperationStatusOK` is explicit success, and `OperationStatusError` is failure. Payload presence alone proves no outcome. Error evidence plus empty status is failure; error evidence plus explicit success or unset is a diagnostic, as are unknown enums. Status-only error uses `error.type=unknown`. Unknown outcomes export OTel Unset, never OK. Model error classes use a closed bounded taxonomy, including cancellation, deadline and stream abort. Explicit tool/retrieval classes must be allowed; classifiers may normalize unsupported values. Raw error text is never exported or wrapped in a diagnostic.

Remove request-to-response model fallbacks. `Operation.Model`, `Meta.RequestModel` and scope model describe the request. Supply `OperationResult.Response.Model` or completed `Meta.ResponseModel` only from observed response evidence; omit it when unknown or when failure precedes response. Adapters cannot fabricate response metadata through request parsing. Metric normalizers receive these separate facts.

Host business errors retain their identity and result. Handle telemetry diagnostics through a separate reporting path, then return the original business result/error. A telemetry diagnostic must not suppress host I/O or replace its error. Physical attempts retain explicitly known final usage even on failure; logical summaries and intermediate streaming updates do not increment it.

## Profile and ownership

`genai.Schema()` and `Contract.Schema()` return an immutable `SchemaDescriptor`
for the fixed schema. Read `ID()`, `Mapping()`, `Fields()` and `Boundaries()` from
it; collection methods return detached copies. `Profile.ID`, `Mapping`, `Fields`
and `Boundaries` are removed. Schema configuration is not accepted by constructors.
`DefaultProfile()` now contains only policy identity, raw permission, series
budget and finite dimensions. Constructors snapshot these settings; `Profile()`
returns a deep copy. Host custom SDK attributes remain outside this contract.

`Schema().Events()`, `Schema().Enumerations()` and `Schema().PayloadChannels()`
provide the fixed catalogue. `Contract.Enumerations()` adds the effective local
error-class declaration; `Contract.PayloadChannels()` resolves actual output
budgets. Present source fields never emit `unknown`: unknown usage is absence.
Unknown structured metadata enum values follow payload policy; raw handling
requires explicit permission. Adapter declarations still identify the fixed
schema, mapping and boundary owners; conformance checks actual export evidence.

The declared built-in policy identity must match the installed sanitizer. Setting
`PayloadPolicyID` alone does not install a policy. Conflicting options are rejected
by the constructor, including a later profile option that mislabels an already
installed policy. Custom policy implementations and their identities are owned
and verified by the host at its composition root.

Run `CheckConformance(tracker.Contract(), declarations, fixtures...)` on exporter-derived fixtures for each composition root. The immutable effective contract replaces the separate Profile and raw flag arguments. It resolves actual context and event budgets; `ObservedEvent.Payloads` must include actual controlled text values, including evaluation reasoning. Do not copy scenario inputs into export evidence.
Replace unbound unit lists with `FieldUnitDeclaration` entries (or inherit the
canonical catalogue with an empty list). Supply mandatory `AccountingEvidence`
per boundary, including actual span and measurement observation counts, not
series counts. Use `IntegerTotal` for integer instruments and `Total` otherwise.
Counter sums alone do not prove write counts; obtain separate write witnesses.
Project actual enums, event owners/fields and named `PayloadEvidence.Value` values from
the controlled exports. A passing report certifies only that fixture's coverage.
Reject duplicate boundary owners, unsupported mapping/profile, mismatched units
and unpermitted raw mode before exercising fixtures. Update metric views,
dashboard queries and alerts for source/currency labels, known-zero histogram
counts, unknown/overflow series and completed-vs-execution spans.

Opaque trace tokens remain separate from business identities. Detached resume
creates a new linked root; it does not mutate or reuse a finished span. Sampling
does not prove whether a predecessor was exported. IDs/references preserve
equality through stable fingerprints in non-raw modes; explicit raw preserves
bounded caller values. There is no cross-process deduplication or execution
registry.


The schema identity is now `metry.genai.semconv.e07f4eb.v1`; older profile identities are rejected.
Use `NewContract(options...)` to validate an instrumentation configuration without registering instruments.
`Tracker.Contract()`, `Contract.Profile()` and `Contract.PayloadChannels()` return detached
values and never expose sanitizer callbacks. Explicit context budgets must fit the minimum
JSON envelope through the input byte bound; event budgets range from 1 through that bound.
The conformance checker verifies the fixture's exported evidence, not arbitrary exporter
behavior or universal absence of PII.

Payload decoding preserves exact JSON number lexemes; hashes of `1`, `1.0` and
`1e0` may differ. Adjacent integers above 2^53 no longer collapse. Structural
truncation preserves retained values; array channels retain their root shape and
tool-extension envelopes count toward depth/node bounds. Malformed structured tool JSON is rejected before policy callbacks;
plain tool text remains a JSON string. See [CONTRACT.md](CONTRACT.md).

Metric helper name arguments become `metrytest.MetricSelector{Name: name}`.
Provide an exact instrumentation scope when names overlap; otherwise a unique
name across all scopes is required. Typed helpers reject wrong aggregations and
attribute accessors reject wrong OTel types rather than yielding zero values.
See [BENCHMARKS.md](BENCHMARKS.md) for the measured baseline and fuzz command.

## Propagation and diagnostic privacy clear break

A remote `metry.attr.type` marker no longer makes baggage trusted log enrichment
or inherited GenAI scope. Configure `propagation.NewPolicy` ingress and egress key
allowlists separately, then install it through Provider options. Local Enrich
continues to write trusted typed span/log fields; network propagation requires a
separate egress permission. Manual OTel baggage does not establish provenance.

Generic root/security diagnostic content uses neutral `diagnostic.Policy`, not
GenAI payload/reference policy. The default suppresses arbitrary error/panic and
security reason text before trace/log fanout. Explicit raw or custom sanitation is
installed with `WithDiagnosticPolicy`/`WithSDKDiagnosticPolicy`. These defaults do
not redefine GenAI payload channels, known outcome classes or reference policy.
Replace `security.RecordSecurityEventWithProvider` scalar calls with
`security.Record(ctx, provider, security.Observation{...})`; metadata is bounded,
block is a host decision and sampling can omit the event. See root `PRIVACY.md`
for host SDK/custom attribute/formatter/exporter escape hatches.

HTTP panic spans and gRPC completion status descriptions follow the Provider's
shared diagnostic policy. Queries expecting arbitrary error/panic descriptions
must use bounded machine status/code fields or explicitly configure sanitized
content. RPC outcomes and original errors are unchanged; the SDK receives a copy
of completion evidence with a processed description.

## Pinned GenAI semantics clear break

The supported upstream commit is `e07f4ebacb08f56db8c4c882d117720333fbca04`.
Both GenAI tracer and meter scopes now declare version
`semconv-genai-e07f4eb+metry-v1` and GenAI Development schema
`https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev`. Include version/schema in
exact `metrytest.MetricSelector` scope matches; a name-only scope does not match.
Resource schema is independent. Full mappings and extension reasons are in
[SEMANTICS.md](SEMANTICS.md).

| Removed contract | New contract | Consumer action |
|---|---|---|
| Implicit locality on interval starts | Required `Operation.Locality`, `RetrievalRequest.Locality`, `AgentInvocation.Locality`: `ExecutionLocal` / `ExecutionRemote` | Declare execution fact; remote CLIENT, local INTERNAL; identity does not override locality |
| One duration instrument for every model observation | `gen_ai.client.operation.duration` only remote intervals; `metry.gen_ai.operation.duration` local/completed | Split latency dashboards; completed timestamps remain recording time |
| `CacheCreationInputTokens`, cache_creation key | `CacheWriteInputTokens`, `gen_ai.usage.cache_write.input_tokens` | Rename Go DTO and queries; no alias |
| Combined `gen_ai.client.token.usage` | Separate `gen_ai.client.inference.operation.input_tokens` and `output_tokens` histograms | Select instruments independently; components remain subsets |
| `gen_ai.token.type` and input_cache_creation | `metry.gen_ai.token.component`, input_cache_write | Update component labels; no new upstream component counter/modality claim |
| Tool duration custom name/labels | `gen_ai.execute_tool.duration`; `gen_ai.tool.name`, `metry.gen_ai.operation.status`, `error.type` | Update queries and canonical span name `execute_tool <name>` |
| Upstream-looking arbitrary tool JSON keys | `metry.gen_ai.tool.call.arguments` / `result` | Any JSON remains supported as extensions; no fabricated object wrapping |
| `ContentPart.Text`, `Result`, `tool_result`, `Message.FinishReason` | `Content`, `Response`, `tool_call_response`; finish-reason field removed | Migrate DTOs and serialized content vocabulary |
| Array truncation envelope | Array prefix and separate `metry.gen_ai.payload.<channel>.truncated` | Check boolean metadata; tool extensions may retain envelopes |
| `ReturnedChunks`, `Distances` | `ReturnedDocuments`, optional `[]RetrievalDocument` with ID/Version/Digest/Relevance | Supply only known document associations; no distance-to-score inference |
| Retrieval provider/source/query custom keys | `gen_ai.provider.name`, `gen_ai.data_source.id`, `gen_ai.retrieval.query.text` | Update keys; document references protected with content disabled |
| `RecordAgentStep`, role/step metadata event | Optional `StartAgent` / `StartWorkflow`, typed `InvocationHandle` | Observe actual intervals; no child usage rollup; invocation metric separate |
| Evaluation metric/score/reasoning keys and old event | name/score.value/score.label/explanation; `gen_ai.evaluation.result` | Update event queries; linked completed-root wrapper remains extension |
| TPS/TBT abbreviated instruments | `metry.gen_ai.client.derived.output_tokens_per_second` / `derived.mean_output_token_interval` | Treat formulas as derived facts, not measured interchunk gaps |
| Upstream-looking cost/media/purpose/cache/sampling auxiliary keys | `metry.gen_ai.*` extensions | Rewrite queries using catalogue; no dualwrite |
| Measurement `<upstream key>.source` / `.unit` | `metry.gen_ai.measurement.<field>.source` / `.unit` | Preserve explicit provenance/unit; keep finite enums |

Invocation finish validates the same outcome/once contract as other handles,
including noop parity and repairable invalid results. Agent names are required
machine labels up to 80 bytes; optional stable resource/conversation/version
references are bounded to 256 bytes. Hosted agent ID uses `gen_ai.agent.id`, local
agent ID uses `metry.gen_ai.agent.id`; neither is generated invocation identity.
Invocation duration labels contain no names, providers, IDs or references.

Retrieval provenance accepts finite signed relevance, nil unknown and pointer-to-zero
known zero; pointers are copied. Lists and complete serialized inputs are bounded
before writes. Document count need not match optional provenance list. Truncation
keeps whole documents and leaves the count intact; no URI fetching or body storage.

Default references remain protected independently of content capture permission.
`Contract.RawEnabled()` describes permitted raw content capture, not the separate
`Contract.ReferenceRawEnabled()` fact. Passing fixture checks covers observed scenarios only.
