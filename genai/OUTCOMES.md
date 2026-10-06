# Observation outcomes

Contract/schema revision: `metry.genai.semconv.e07f4eb.v1`.
This document defines local recording lifecycles. Callers own business execution.
Field/instrument mappings are in [SEMANTICS.md](SEMANTICS.md); removals and
renames are in [MIGRATION.md](MIGRATION.md).

## Recording lifecycle

`OperationHandle`, `ToolHandle`, `RetrievalHandle` and `InvocationHandle` own one
local telemetry interval. `Finish` validates a final caller result; returned
validation errors write nothing and remain correctable. Malformed tool payload
JSON may instead add a bounded payload diagnostic while completing a valid
caller outcome. A successful `Finish`
or `Close` is terminal; later calls return `ErrFinalized`, including streaming
updates. `Close` reports an unknown outcome without inventing usage, results or
returned documents. Nil/zero handles return a bounded missing-recording diagnostic.
Concurrent finalization is serialized; only one valid call commits. Do not copy
handles after use. Caller I/O, cancellation and retries remain outside metry.

A host `PayloadPolicy` or `ToolErrorClassifier` panic during `Finish` terminates
an existing span with error outcome, `error.type=unknown` and the declared
`metry.observation.rejected` event (`field=callback`, `rule=panic`). The original
panic reaches the host; raw panic content is never exported by this path, even
with raw diagnostic opt-in. The handle becomes terminal. Start/completed-result
paths sanitize and classify before creating their span; a callback panic creates
no observation. `RecordTraceIO` borrows the context span and leaves its outcome
and lifetime to the host. Diagnostic-policy sanitizer failures retain guarded,
fail-closed semantics. Custom SDK failures are outside this callback guarantee.

See [INTEGRATION.md](../INTEGRATION.md) for shared-object concurrency, callback
requirements, explicit noop selection and SDK shutdown ownership.

## Outcome table

`Meta`, `OperationResult`, `ToolResult`, `RetrievalResult` and `InvocationResult`
carry `Outcome`. Empty is unknown; success must be explicit.

| Status input | Error evidence | Exported status | OTel status | error.type |
|---|---|---|---|---|
| `ok` | absent | `ok` | Ok | absent |
| `unset` or empty | absent | `unset` | Unset | absent |
| `error` | absent | `error` | Error | `unknown` |
| `error` or empty | present | `error` | Error | bounded observed class |
| `ok` or explicit `unset` | present | diagnostic, no writes | no mutation | no mutation |
| unsupported enum | any | diagnostic, no writes | no mutation | no mutation |

Model `ErrorType` accepts `unknown`, `error`, `cancelled`, `deadline_exceeded`,
`stream_aborted`, `timeout`, `rate_limit`, `quota_exceeded`, `validation`,
`unavailable`, `permission_denied`, `unauthenticated`, `not_found`, `conflict` and
`internal`. Invalid model classes reject rather than normalize. The shared
schema enumeration also covers tool/invocation classes such as `paused`; it is
not the exact model subset. `Contract.Enumerations()` additionally resolves the
installed local error-class declaration.
Tool/retrieval/invocation explicit `ErrorClass` must belong to the configured
allowlist; classifiers may normalize unknown classes. Default classification
recognizes cancellation/deadline through `errors.Is`; model producers supply the
observed class explicitly. Business error text is never exported by these paths.
Async deferred results are separate linked observations, not local I/O handles.
Evaluation failed/skipped status describes the returned assessment and has no
score; it does not claim that recording the observation itself failed.

## Metadata and accounting

Request model comes from the operation/request or local scope. Response model
is an explicitly observed caller fact; there is no request/scope fallback.
`ProviderAdapter.ParseResponse` returns `ResponseObservation` with payload,
usage, metadata and outcome; its response metadata replaces request-side values.
Adapters belong to the host and must meet its concurrency requirements.

Live handles measure elapsed duration locally using the monotonic clock;
producers cannot supply it. Completed operation/stream observations carry the
supplied known duration as a metric while the span covers recording only.
Physical attempts account known final resources even on failure. Logical
summaries skip resource and duration accounting. Inclusive usage/component rules
and units are defined in [CONTRACT.md](CONTRACT.md) and [SEMANTICS.md](SEMANTICS.md).

## Streaming presence and cancellation

`ValidateStreamingUpdate` checks intermediate cumulative usage without committing
metrics. Only final `Finish` or a completed-result recording accounts final usage;
partial updates never fill an absent final total.

`FirstTokenPresent=false` means the first token was not observed or its timing is
unknown. A known zero TTFT is represented by `FirstTokenPresent=true, TTFT=0`.
Failure/cancellation after the first token preserves the observation. A first
token at 20ms followed by cancellation at 100ms records TTFT 0.020s even when all
final usage is absent. Cancellation before a first token records no TTFT. Neither
case invents token usage or cost; derived throughput requires known final output
tokens and a positive generation window. Timing presence and final usage presence
are independent. Record these facts explicitly; cancelling a context alone does
not finalize a recording handle or infer an error outcome.
