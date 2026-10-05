# Instrumentation contract migration

This is a clear break. There are no scalar-usage wrappers or legacy evaluation
entry points. Host adapters must translate their own types at the composition
root; metry does not own execution, retry decisions, storage or pricing.

| Previous contract | Current contract | Names / units / boundary | Consumer action |
|---|---|---|---|
| Scalar `Usage`, zero treated as missing | Typed `Measurement[int]` / `Measurement[float64]` with explicit presence/source/unit | Existing total/component attributes remain; each present measurement adds `.source` and `.unit` | Replace literals with `Observe`; use zero-value measurement for absence and present zero for a known zero |
| Implicit provenance | Per-measurement measured/provider-reported/estimated provenance | Span source fields and `metry.usage.source` metric label distinguish sources | Update dashboards to group/filter provenance; do not treat estimates as measured |
| Inclusive totals mixed with subsets | Totals stay inclusive; overlapping subsets are separate | `gen_ai.client.token.usage` vs `metry.gen_ai.client.token.component.usage`, unit token | Remove totals-plus-components dashboard sums; normalize exhaustive exclusive buckets once in the host adapter; incomplete buckets leave total absent |
| Implicit cost currency | Explicit declared currency and independently sourced cost | `gen_ai.cost`, `gen_ai.cost.currency`, currency unit; no conversion | Declare currencies in the profile; group currencies separately; host retains prices, conversion and quotas |
| Recording a completed result treated as an execution span | `StartOperation` before I/O and local `Finish` after I/O | Execution duration uses local monotonic time; completed helpers mark `metry.observation.kind=completed` | Choose one owner for each boundary; do not derive execution latency from completed-observation timestamps |
| Logical totals added again after retries | Physical operations/attempts account resources; logical summaries do not | Causal identity remains span-only; failed physical retries still count | Supply stable operation IDs and distinct attempt IDs; remove summary accounting from host adapters |
| Scalar streaming output count and separately repeated TTFT/usage | Final DTO uses `Usage`; local handle commits once | TTFT/total duration measured from the same start; `FirstTokenPresent` distinguishes zero/unknown | Intermediate updates must not increment totals; do not record the same final usage or TTFT through two paths |
| Mandatory handle and numeric-only `RecordEvaluations` | Separate `EvaluationRecorder.Record` with status, score xor category, optional trace and experiment association | New `evaluation` root, optional link; failed/skipped contain no score | Remove old calls; supply observation ID, ready result and optional full experiment/case/trial/content references; keep artifacts outside metry |
| Raw names, URLs and IDs accepted as span metadata | Declared safe machine labels or bounded fingerprints in non-raw modes | Attributes and span names follow local policy; metric unknown/overflow buckets remain bounded | Declare static non-sensitive labels, update queries for wire fingerprints; do not allowlist dynamic URLs/user IDs |
| Arbitrary JSON keys survived redact/hash | Whole-object sanitation unless static schema keys are explicitly allowlisted | Messages/tool args/results are valid bounded JSON; hash is not anonymity | Review key allowlists, remove assumptions about unconditionally preserved object shape; raw export requires explicit opt-in |
| Serialized JSON sliced as text | Structural prefix/envelope with `truncated: true` | Minimum byte budget fits the marker-only envelope; text channels retain text truncation | Parse the truncation envelope, configure a sufficient budget; do not attempt to repair invalid JSON fragments |
| Unbounded sanitizer inputs or inline media bodies | Input byte/depth/count limits; media references only, no fetching | Limits precede sanitizer work; even raw mode rejects inline declared media bodies | Keep binary content/artifacts in host storage; submit bounded opaque references |
| Arbitrary adapter error messages wrapped into diagnostics | Bounded request/response `parse_failed` diagnostic | Original adapter errors are not exported or wrapped | Handle original errors within the host adapter; do not depend on `errors.Is` matching arbitrary adapter errors |
| Retrieval output attributes written without validation | `RetrievalHandle.Finish` returns an error before invalid writes | Finite distances; bounded arrays; nonnegative count; no imposed distance scale | Handle telemetry diagnostics separately from business results; check returned errors |
| Completed observation or standalone streaming completion without operation name silently dropped usage metrics | Effective operation name required, resolved from caller scope when omitted | Required `gen_ai.operation.name`; malformed name rejected before span/metric writes, including noop paths | Supply an operation name or `Scope.Operation`; model/purpose alone is insufficient |

## Outcome and handle clear break

The following signatures are removed, with no compatibility wrappers:

- `Runtime.StartTool(ctx, call, opts...) (context.Context, ToolEnd)` and `Runtime.RecordToolResult(ctx, result)` become `Runtime.StartTool(ctx, call, opts...) (context.Context, *ToolHandle, error)` and `ToolHandle.Finish(result) error`.
- `Tracker.StartRetrievalSpan(ctx, name, request, opts...)` and `Tracker.RecordRetrievalResult(ctx, result)` become `Tracker.StartRetrieval(ctx, name, request, opts...) (context.Context, *RetrievalHandle, error)` and `RetrievalHandle.Finish(result) error`. Runtime exposes the same start method.
- Adapter `ParseResponse(any) (Payload, Usage, error)` becomes `ParseResponse(any) (ResponseObservation, error)`. Return all observed response facts from that call: `Payload`, `Usage`, `Metadata: ResponseMetadata{Model: actualModel}`, `Status` and `ErrorType`. Domain request/response types require no metry interface, and adapters require no mutable request state.

All operation, tool and retrieval handles expose `Close() error`. Start before host I/O; finish after I/O. Defer `Close` to cover exits that cannot provide a valid result. A successful finish already closes the handle, so a deferred close returns `ErrFinalized`; it produces no additional writes. Invalid finish is repairable and produces no final usage or outcome writes. Close commits unknown without fabricated usage, tool result or retrieval chunks. Live durations use a local monotonic clock; completed observations retain their supplied known duration and completed marker.

`Meta.Status`, `OperationResult.Status`, `ToolResult.Status` and `RetrievalResult.Status` use `Outcome`. Empty and `OperationStatusUnset` are unknown, `OperationStatusOK` is explicit success, and `OperationStatusError` is failure. Payload presence alone proves no outcome. Error evidence plus empty status is failure; error evidence plus explicit success or unset is a diagnostic, as are unknown enums. Status-only error uses `error.type=unknown`. Unknown outcomes export OTel Unset, never OK. Model error classes use a closed bounded taxonomy, including cancellation, deadline and stream abort. Explicit tool/retrieval classes must be allowed; classifiers may normalize unsupported values. Raw error text is never exported or wrapped in a diagnostic.

Remove request-to-response model fallbacks. `Operation.Model`, `Meta.RequestModel` and scope model describe the request. Supply `OperationResult.Response.Model` or completed `Meta.ResponseModel` only from observed response evidence; omit it when unknown or when failure precedes response. Adapters cannot fabricate response metadata through request parsing. Metric normalizers receive these separate facts.

Host business errors retain their identity and result. Handle telemetry diagnostics through a separate reporting path, then return the original business result/error. A telemetry diagnostic must not suppress host I/O or replace its error. Physical attempts retain explicitly known final usage even on failure; logical summaries and intermediate streaming updates do not increment it.

## Profile and ownership

`DefaultProfile()` provides the canonical field catalogue, field stability,
encodings, units, operation boundary declarations and finite dimension policy.
The profile is snapshotted by constructors. It cannot omit or reinterpret owned
fields, or declare unknown fields under the same schema identity. Local safe
label values and bounded series budget are configurable. Host custom SDK
attributes retain their own names and remain outside this checked contract.

Canonical boundaries are checked alongside fields: do not remove, rename or
change accounting semantics under this profile identity. Configure finite safe
dimension values for application-specific operation names. `Profile.Events()`
declares event names, instrumentation ownership and allowed attributes. These
declarations do not transfer ownership of business execution to the library.

`Profile.Enumerations()` and `Profile.PayloadChannels()` return detached enum and
input/encoding/budget-setting declarations. Present source fields never emit
`unknown`: unknown usage is absence. Unknown structured metadata enum values
follow payload policy; raw handling requires explicit permission. Channels name
the runtime setting controlling output budget rather than introducing another
independent payload limit.

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


The schema identity is now `metry.genai.effective`; older profile identities are rejected.
Use `NewContract(options...)` to validate an instrumentation configuration without registering instruments.
`Tracker.Contract()`, `Contract.Profile()` and `Contract.PayloadChannels()` return detached
values and never expose sanitizer callbacks. Explicit context budgets must fit the minimum
JSON envelope through the input byte bound; event budgets range from 1 through that bound.
The conformance checker verifies the fixture's exported evidence, not arbitrary exporter
behavior or universal absence of PII.

Payload decoding preserves exact JSON number lexemes; hashes of `1`, `1.0` and
`1e0` may differ. Adjacent integers above 2^53 no longer collapse. Structural
truncation preserves retained values and includes its envelope in depth/node
bounds. Malformed structured tool JSON is rejected before policy callbacks;
plain tool text remains a JSON string. See [CONTRACT.md](CONTRACT.md).

Metric helper name arguments become `metrytest.MetricSelector{Name: name}`.
Provide an exact instrumentation scope when names overlap; otherwise a unique
name across all scopes is required. Typed helpers reject wrong aggregations and
attribute accessors reject wrong OTel types rather than yielding zero values.
See [BENCHMARKS.md](BENCHMARKS.md) for the measured baseline and fuzz command.
