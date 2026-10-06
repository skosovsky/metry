# Task 23 execution ledger

Source: `task23-metry-review-remediation.md`. This ledger tracks implementation;
the source remains the acceptance specification. No release or production push.

## Sequential tasks and acceptance

Each row is one task and one commit. Work starts only after the previous row is
accepted and committed. Before each commit, two fresh independent reviewers who
did not implement the task review the same diff: completeness must be 100%, and
correctness must have no unresolved findings. Reviewers do not see each other's
reports. Any fix triggers both reviews again. Record reports and validation here.

| Task | Coverage | Acceptance evidence | Commit | State |
|---|---|---|---|---|
| T01 Contracts and decisions | R01–R08, D01–D16 | Explicit status/restore/enrichment/scope contracts, complete decision register, breaking changes, sequential coverage of all DoD items | `docs: remediation contracts` | accepted |
| T02 Generic span completion | R01 | AAA real SDK/public-only wrapper/noop; Error retained, unknown remains Unset; all shared helper callers audited | `fix: span status` | accepted |
| T03 Isolated release workflow | R02, D16 | Temporary repo/bare remote: clean-state rejection, explicit manifests, only run-created tags, failure cleanup, portable edits; release/recovery docs | `fix: release staging` | accepted |
| T04 Explicit recorder construction | R03 | Catch-all constructor removed; invalid budgets/profile/policy/instruments reach caller; explicit noop; composition examples updated | `fix: recorder errors` | accepted |
| T05 Durable trace restoration | R04 | Own W3C decoding across W3C/baggage/custom transports, exact reference restore over foreign ctx; policy rejection returns error and original ctx | `fix: trace restore` | accepted |
| T06 Raw enrichment and bounded propagation | R05, D12, D13 | Raw string span/log/private/transport AAA, literal percent stability, invalid keys; ordinary allowlisted W3C strings; deterministic byte/member overflow tests | `fix: baggage values` | accepted |
| T07 Private GenAI scope | R06 | Package/Tracker scope and direct Meta sentinel privacy across parent/child/logs; originals survive merge, safe/raw opt-in, local precedence/remote trust | `fix: scope privacy` | accepted |
| T08 GenAI API cleanup | D01–D06 | Schema descriptor separate from runtime settings, intact conformance/introspection, typed SDK scope/schema/version, recorder naming, validation-only updates, single presence-aware TTFT owner, byte-limit names; removals migrated everywhere | `refactor: genai contracts` | accepted |
| T09 Core API and lifecycle cleanup | D07–D11 | Domain helpers and unused maps removed; disabled async cannot cause fake links; concurrent writer AAA/race and nil behavior; terminal shutdown/lifetime docs and tests | `refactor: core lifecycle` | pending |
| T10 Callback failure safety | D14 | All host callback paths audited; panic preserves host panic, completes owned span with bounded categorical error, never raw panic/success; regression evidence | `fix: callback lifetime` | pending |
| T11 Independent evaluation provenance | D15 | Online grader/target references without fabricated experiment tuple; experiment tuple remains complete; validation/export privacy and BYOT adapter example | `fix: evaluation provenance` | pending |
| T12 Consumer documentation and stream semantics | R07, R08, documentation checklist | One normative schema ID, useful docs consolidated, stable versioned links, migration and all examples accurate; privacy matrix and concurrency/lifetime/nil/noop ownership; cancelled stream with/without observed TTFT AAA | `docs: telemetry contracts` | pending |
| T13 OTLP semantic fixture | integration checklist | Real serialized resource/scope/schema, known-zero vs unknown usage, histogram/cost, parent/link, error outcome, no raw sentinel; evidence limits documented | `test: otlp semantics` | pending |
| T14 Full acceptance | complete DoD | `make test`, `make lint`, `make test-examples` across modules; targeted races and isolated release checks; two independent whole-DoD reviews; source/ledger statuses backed by evidence | `chore: remediation checks` | pending |

## Planned public contracts

These decisions describe the target behavior; pending rows are not claims about
the current implementation. Consumer docs become normative as their code lands.

* Generic span end only ends. It never infers Ok, whether an SDK getter exists or
  not. Explicit result owners (HTTP/executor/GenAI/linked callback) may assign Ok
  for a known successful result. Errors already recorded must remain errors.
* Durable snapshot wire format is W3C traceparent/tracestate and is decoded with
  TraceContext independently of transport. The provider's remote-trace policy
  also governs durable restore. Rejection returns `ErrTraceSnapshotRejected` and
  the original context. Successful restore must match captured trace ID, span ID,
  flags and tracestate; baggage/local enrichment are retained. Nil/zero provider
  follows the existing trace-only default policy.
* Enrich accepts raw local scalar values. Raw validation is consistent with
  NewMemberRaw; percent encoding happens at transport. Local provenance/log/span
  values survive transport baggage overflow. Invalid keys remain rejected.
  Egress/visible baggage uses sorted keys and retains a fitting prefix within
  pinned SDK member/byte bounds; stops at first overflow. Diagnostics are bounded
  categorical counts, containing no values. Allowlisted ingress without a type
  marker is string; explicit invalid type markers remain rejected. Local wins.
* WithScope stores original defaults in a private context entry, without writes
  to parent span, log enrichment or baggage. Both forms have identical behavior;
  the receiver's policy applies when its tracker emits telemetry. Scope lookup
  merges private local defaults over admitted enrichment fallback; explicit Meta
  wins over scope. No fingerprint before merge. Every tracker output applies its
  metadata policy; direct trusted Enrich remains an explicit raw host action.
* Constructor configuration errors are returned. Host selects an explicit noop.
  Unknown usage remains unknown; known zero remains measured. Cancellation after
  the first observed token preserves TTFT; final usage is independent.

## Final decision register

| ID | Decision and consumer rationale | Owner task |
|---|---|---|
| D01 | Expose fixed schema as immutable descriptor with detached fields/boundaries; Profile retains only policy/raw permission/series budget/dimensions. Contract exposes descriptor and effective settings. Fixed schema needs introspection, not writable pretend-schema input. | T08 |
| D02 | Remove MeterTracer init bridge, construct instruments via typed Provider.SDK with identical name/version/schema. Keep private async representation bridge; do not expose internal handles. | T08 |
| D03 | Rename Runtime to Recorder, including constructors and NoopRecorder; consumer-owned narrow interfaces remain possible. Tracker instruments intervals and never executes work. No alias preserving old Runtime API. | T08 |
| D04 | Rename handle RecordStreamingUpdate to ValidateStreamingUpdate; validation produces no interim accounting. | T08 |
| D05 | Remove scalar RecordTTFT and its recorder surface; final presence-aware Timing is the single TTFT export owner. No evidenced consumer needs early export. | T08 |
| D06 | Rename WithMaxContextLength to WithPayloadByteLimit and WithMaxEventLength to WithEventTextByteLimit, including effective budget declarations. Keep telemetry wire attribute names/schema identity unless changed semantics require versioning; enumerate any declaration name changes in migration. | T08 |
| D07 | Remove DoctorID/PatientID helpers. Generic consumers use StringAttribute; no new domain dependency or change to neighboring services in this repository. Retain generic TenantID/SubjectID. | T09 |
| D08 | Keep NoopAsyncHandle as explicitly disabled private representation with no valid trace identity. Disabled link operations do not export or fabricate links; serialization cannot imply real origin. Evaluation trace reference must still be real. | T09 |
| D09 | Writer callbacks may run concurrently within RecordLinkedSpan; caller must join workers before callback returns. Protect shared state; first non-nil SetError retained, nil does nothing. Writes after callback lifetime are inert; callback return error remains the returned error and controls failure outcome. | T09 |
| D10 | Remove unread instrument maps; retain collision names registry only. | T09 |
| D11 | Keep terminal, idempotent owned SDK shutdown: concurrent callers wait for same first result; first cancelled call remains terminal. Use live shutdown ctx, document bounded timeout and borrowed SDK host ownership. Provider owns lifecycle state. | T09 |
| D12 | Admit allowlisted ordinary W3C baggage as string when no private marker exists; preserve reserved-key protection/local precedence. | T06 |
| D13 | Sorted fitting-prefix retention within pinned byte/member bounds; overflow explicit in bounded categorical diagnostics. Local trusted ledger unaffected. | T06 |
| D14 | Host policy/classifier panic propagates after owned span is completed as error with categorical diagnostics, no raw panic. Diagnostic policy's existing guarded failure behavior stays fail-closed. Audit start and finish paths; never leave a span stranded after marking finished. | T10 |
| D15 | Separate EvaluationProvenance (independently optional grader/target references) from complete EvaluationAssociation experiment tuple. Host maps opaque caller references; no evaly import. Evidence: ../evaly/grader.go Assess accepts only graders/view factory, independently of experiment; ../evaly/workflow_integration_test.go TestWorkflowOnlinePartialAssessment demonstrates online partial grader results. Add metry adapter example for Assess-style ready results, not an eval runner. | T11 |
| D16 | Run release edits/commit in isolated temporary checkout with cleanup trap; portable manifest editing and explicit tag refs, original checkout stays attached and clean. Test local bare remote only. Document supported Bash/Go/Git platforms and failure recovery. | T03 |

## Breaking changes

Generic End no longer invents success; snapshot policy rejection is an error;
scope helpers stop raw enrichment/transport side effects; ordinary allowlisted
W3C strings gain ingress support; overflow retention becomes deterministic.
Remove RuntimeFromProvider catch-all, Runtime names, scalar RecordTTFT,
DoctorID/PatientID and writable schema descriptor fields. Rename streaming
validation and payload byte options/declarations. Split online provenance from
experiment association. No legacy wrappers. Migration must list the final exact
public signatures and semantic changes after implementation.

## Acceptance reports

T01 accepted by independent agents `t01_completeness` (100%, no gaps) and
`t01_correctness` (no findings). Both inspected this ledger and the source task;
correctness additionally inspected existing contracts and evaly consumer paths.
Reports agreed on complete R/D/DoD coverage and grounded D15. Limits: specification
review only, no runtime/portability/OTLP proof; those remain in implementation rows.

T02 implementation evidence: shared helper caller audit found Provider.StartSpan
as the sole generic end caller; both SDK-introspection helpers and obsolete tests
removed. Remaining SpanOK calls belong to explicit callback/result owners.
New AAA tests cover SDK/public-only wrappers, Unset/Error/Ok, repeat End and noop.
Copied regressions into isolated pre-change HEAD archive: Unset/sdk,
Unset/public and Error/public fail with actual Ok, confirming old violations.
Root `go test -race ./...` passed; root lint passed with 0 issues using writable
temporary caches. Independent `t02_completeness` accepted 100%, no gaps;
`t02_correctness` accepted with no findings. Both independently ran targeted
tests and audited explicit result owners; neither repeated full race, lint or
pre-change reproduction. No remaining T02 findings.

T03 accepted after fixing initial review finding (isolated clone dropped local
tag/GPG signing configuration). Effective signing settings now transfer and both
commit/tag signing failures are regressions. Full suite passed macOS Bash 3.2,
Bash 5.3 and Linux amd64 Docker golang:1.25.5 with network disabled; syntax and
diff checks passed. Pre-change script failed the same suite on local tag leakage.
Independent repeat reviews: `t03_completeness` 100%, independently reran macOS
and Linux suites; `t03_correctness` no findings, independently reran macOS suite.
Evidence includes dirty/staged/untracked preflight, exact staged manifest, version
and replacement edits, shared root/module commit, unrelated tag exclusion,
atomic remote rejection, malformed manifest, cancellation, root-only break and
signer failures. Limits: no production publication, real keys, actual network
ambiguity or uncatchable process termination. Recovery docs state these limits.

T04 accepted: `t04_completeness` 100%, `t04_correctness` no findings. Both ran
independent targeted constructor/noop tests. Parent ran GenAI race suite, root
lint (0 issues), all 9 examples and diff check. Isolated old-HEAD repro returned
payload_budget error from strict constructor but genai.noopRuntime from removed
convenience constructor. Current AAA covers budgets, unsupported profile,
policy mismatch/raw permission, nil provider, failures at all 13 instrument
registrations and export presence for enabled/disabled composition. No live
backend failure testing; no evidence claimed beyond SDK boundary and local exports.

T05 accepted: `t05_completeness` 100%, `t05_correctness` no findings; both ran
independent snapshot/root/propagation targeted tests. Parent root race suite and
lint (0 issues) passed. Old-HEAD archive failed new baggage-only restoration
tests over both empty and foreign contexts. Current tests prove exact W3C
reference (IDs/flags/state/remote), transport independence including panic-on-use
custom transport, retained caller baggage/private values, owned+borrowed policy
rejection with identical original ctx, nil/zero defaults. Existing parser/size
and capture-without-baggage tests retained. No external backend or fuzz proof.

T06 accepted by `t06_completeness` (100%, no gaps) and `t06_correctness` (no
unresolved findings). Both independently checked pinned SDK source and ran root,
propagation and baggageutil tests. Root full race suite passed; final validation
microchange followed by root/propagation/helper race PASS and root lint 0 issues.
Isolated old-HEAD tests fail spaces/percent/Unicode/comma retention. Current AAA
proves six raw values in span/log/private/wire, invalid keys/UTF-8, optional
string markers vs invalid explicit markers, local/reserved/allowlist protection,
shuffled 64-member prefix, exact/over 8192 bytes with commas and encoding,
overflow ledger recovery, merged ingress/egress and count-only diagnostics.
Benchmark on Apple M1 Max (200ms, indicative, no agreed performance SLA):
8 attrs baseline 4352ns/8160B/42 allocs vs current 7465ns/8196B/123 allocs;
70 attrs baseline 141928ns/348147B/417 allocs vs current 74687ns/96634B/880 allocs.
Small-batch overhead is documented; large-batch maps no longer clone per member.
Limits: no external backends/fuzz; incoming wire is first bounded by pinned SDK
parsing, library-owned retention covers local baggage, merged ledger and egress.

T07 accepted by independent `t07_completeness` (100%, no gaps) and
`t07_correctness` (no unresolved findings). Completeness ran GenAI/propagation
scope/trust tests; correctness independently ran GenAI/propagation/gRPC tests
and GenAI/propagation races. Parent root race suite, root lint (0 issues), all
9 examples and diff check passed. Isolated pre-change HEAD archive fails the
new default/package and default/tracker tests on raw parent/child log values.
Current AAA matrix covers direct Meta/package/Tracker with actual default,
declared safe labels and raw opt-in: parent/child spans, logs, baggage and default
metrics. Private originals survive export; emitter policy is independent of
creator, partial merges are immutable, explicit Meta/private/admitted enrichment
precedence and arbitrary baggage rejection are preserved. Docs state raw Enrich
boundary and worker reapplication. Limits: no absolute bug-free claim or external
backend verification; serialized OTLP fixture remains T13.

T08 accepted after correcting first-review documentation findings: mechanical
rename had altered historical removed Runtime names and one README sentence.
Repeat independent reviews: `t08_completeness` 100%, no gaps;
`t08_correctness` no unresolved findings. Both rechecked the current diff and
independently ran GenAI tests; correctness also ran root package tests. Parent
root `go test -p 2 -race ./...`, root lint (0 issues), gRPC/OTLP module tests and
all 9 examples passed. Build parallelism was bounded after a transient temporary
storage failure; subsequent checks passed. Existing conformance/export fixtures
and pinned scope/schema assertions remain; new AAA checks detached immutable
schema fields/boundaries, effective vs fixed error enums, resolved byte getter
and channel names, exact typed SDK scope/version/schema and zero-provider noop
SDK behavior. Existing stream tests prove validation-only updates, known-zero
TTFT and single Finish accounting. All public Recorder names and byte-limit
options/declarations migrated without aliases; scalar TTFT, pretend-schema input,
MeterTracer bridge and obsolete errors/tests removed, private async bridge kept.
Migration records exact removals/renames and unchanged wire semantics. Limits:
no external backend proof or absolute absence-of-bugs claim; complete cross-module
release/final acceptance remains T14.
