# Host observation mapping

This contract is demonstrated by the runnable `examples/observation_mapping`.
The host retains its request, response, stream and assessment types. Mapping is
explicit; metry observes completed facts and does not run or reconcile execution.
`OperationHandle.Finish` now accepts optional host attributes. No automatic cross-producer conversion occurs.

## Model outcomes

`host.model.provider_outcome` is a host-owned string attribute with the finite
values `completed`, `refusal`, `incomplete`, `failed`, `cancelled`, `deadline`,
`unknown`. `host.model.domain_success` is an optional host-owned boolean; false
is known failure, absence is unknown. Neither is a metric dimension.

| Host state | Error evidence | Operation status | error.type | Provider fact |
|---|---|---|---|---|
| completed | absent | ok | absent | completed |
| refusal / incomplete | absent | ok | absent | refusal / incomplete |
| failed | any | error | unknown or observed class | failed |
| cancelled | any | error | cancelled | cancelled |
| deadline | any | error | deadline_exceeded | deadline |
| unknown | absent | unset | absent | unknown |
| completed / refusal / incomplete / unknown | present | error | bounded class | original state |
| unsupported state | any | reject before recording | none | none |

Operation success means the invocation returned successfully, not that its answer
satisfied the domain. Refusal and incomplete remain observable with successful
transport. Result+error preserves response, known usage and provider fact. Error
classification uses `errors.Is` for cancellation/deadline, otherwise `unknown`;
raw errors are never attributes. Mapping returns telemetry separately from the
business result and original error.

Completed observations pass these attributes to `RecordOperation`. Live handles
pass them to `Finish(result, attrs...)`. Its mutex commits host attributes together
with the accepted terminal result after all result validation. Invalid, repeated
or losing concurrent callers do not write terminal metadata. Host fields remain
outside the controlled payload policy: the host must sanitize arbitrary fields
before passing them, and must avoid metry-owned keys. The example permits only
the finite provider enum and an optional boolean, never arbitrary metadata.

## Usage and terminal ownership

| Host evidence | Mapping / rejection | Export |
|---|---|---|
| no measurement | zero `Measurement` | no value or metric write |
| known zero | `Observe(0, unit, source)` | typed zero, one observation |
| partial result | retain only present measurements | no inferred missing total |
| inclusive input/output | validate unit/source | token totals |
| cache/reasoning component | retain separately | components, never added to totals |
| caller cost | explicit currency/source | cost counter, no pricing lookup |
| cumulative stream updates | validate, never add | no intermediate accounting |
| complete ordered delta sequence | host sums with integer overflow check | final usage only |
| delta duplicate/gap/out-of-order/mixed mode | reject normalization | no fabricated total |
| absent terminal usage | absence, even after partial updates | no inferred accounting |

The example delta sequence has sequence numbers starting at one, one mode, a
terminal marker on its last item and no earlier marker. Empty/incomplete streams
are rejected. Integer overflow and negative values reject; known zero is retained.
Cumulative snapshots may repeat but must be nondecreasing and sequenced; only the
terminal snapshot is final. A terminal snapshot is a host assertion of completeness,
not a fact inferred by metry. Other source protocols need their own explicit rules.

One physical model attempt has one accounting owner and one `OperationHandle`.
The host rejects multiple owners before starting a recording. Logical operation
and step IDs are distinct from attempt IDs. Logical summaries use
`IdentityLogical`; workflow step observations are not model accounting owners.
Separate real attempts, including failed attempts, retain their own known costs.
Do not combine `RecordOperation`, `RecordStreamingCompletion` and `Finish` for
one boundary. `Finish` commits once; an invalid result can be corrected and a
concurrent/repeated successful terminal is rejected with `ErrFinalized`.
Returning a route handler does not finish a stream. The host explicitly accepts
its terminal event; cancellation alone does not finalize the handle. Observed
TTFT before cancellation survives without inventing final usage.

## Ready assessments and host delivery

| Host assessment | Evaluation status | Result |
|---|---|---|
| scored, including zero | succeeded | Score pointer |
| assertion, including false | succeeded | Outcome `true` / `false` |
| unavailable or grader_error | failed | no Score/Outcome |
| skipped | skipped | no Score/Outcome |
| unsupported state | reject | no observation |

One metric/assertion per observation. The host supplies a stable ObservationID
per metric and trial; delivery retry reuses it. Different metrics/trials have
different IDs. The optional experiment/dataset/case/trial tuple is all-or-none;
target and grader references are independent, may be absent and are opaque.
Score has no universal range. Validate before recording. Standalone evaluations
are new roots; linked roots link the supplied valid reference without mutating
the target or depending on its export. IDs/references, including string outcome,
are fingerprinted unless explicit raw capture is enabled; reasoning follows the
recorder's payload policy and event text budget. False is a valid result even
when its exported label is fingerprinted.

Repeated `EvaluationRecorder.Record` emits repeated observations with stable
identity. It is not a deduplication service. The example host sink atomically
stores one measurement per ID and rejects a conflicting replay; an acknowledgement
lost after commit can be retried safely against this sink. It is an in-memory
protocol illustration, not durable storage or a production outbox. A real host
owns persistence, transactions, reconciliation and all retry decisions. Sink
idempotency does not make telemetry transport exactly-once.

## Export assertions, SDK and privacy

Metry fields, types, units and scope/schema remain as declared by
[SEMANTICS.md](SEMANTICS.md) and the executable `Contract`. Token span values
are integers with `metry.gen_ai.measurement.<field>.source` and
`metry.gen_ai.measurement.<field>.unit` provenance; token histogram units are
`{token}`, duration `s`, and cost uses the declared currency unit. Tests assert
actual exported data, counts and sums, not names alone or input DTO equality.
Host scopes retain their own schema identity; shared ancestry does not make
scope schemas interchangeable. See [CONTRACT.md](CONTRACT.md).

`FromSDK` borrows providers. Its `Shutdown`/`ForceFlush` do not close/flush the
host SDK; `FlushDependencies` is explicit. No global SDK mutation is needed.
Each sink has its own capture policy and SDK routing. Metry policy covers only
its controlled channels, not a host span or arbitrary extra metadata, even on a
shared SDK. Host data outside those channels requires host redaction before
writing. See [privacy](../PRIVACY.md) and [SDK ownership](../INTEGRATION.md).

## Consumer gates and migration

`make test-integration` prepares module artifacts from the current source and
runs the same observation mapping and OTLP/gRPC semantics against fresh consumers
with `GOWORK=off` and no replacements. Every discovered module participates.
`make test-live` separately verifies an explicit published baseline (`v0.10.0`);
`METRY_PUBLISHED_VERSION` selects another published version. Historical baseline
success does not certify the current candidate. Expectations are literals justified
by this contract; changing dependencies cannot regenerate them. See
[verification](../docs/verification.md). No source protocol JSON is introduced.

Consumers should replace status casts with the outcome table, pass explicit
measurements, assign one owner per attempt, and finish streams on terminal events.
Preserve result+error, provide stable assessment identities and explicit tuple
and independent references, and implement dedup/privacy in their own host.
Update custom interfaces/method values that described `Finish(OperationResult)`
to `Finish(OperationResult, ...metry.Attribute) error`. Existing single-result
calls continue to compile; the method type has changed. Migration also affects
host glue and telemetry queries: query provider outcome and domain success separately from `metry.gen_ai.operation.status`.

### Live terminal migration

Previously, setting SDK attributes before a separate `Finish(result)` could
publish metadata from a losing terminal caller. Remove that separate SDK write
and pass only checked host metadata to `handle.Finish(result, attrs...)` instead.
No compatibility wrapper or second recording path is introduced.

| Consumer dependency set | Expected result |
|---|---|
| Checkout / newly published contract | All mapping and OTLP fixtures pass |
| Older contract lacking variadic Finish | Live terminal fixture does not compile; this is the declared API break |

The source artifact gate must pass against the current contract; historical published
baselines are separate evidence and expectations are not relaxed.
