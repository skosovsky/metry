# Privacy and trust boundaries

Metry observes host operations and decisions. It does not infer business rights,
tenants or sensitive-data categories, and does not execute security enforcement.
Privacy guarantees apply to the library-controlled channels below.
HTTP spans are ended before an original panic is re-raised, preventing the SDK
from adding a second raw exception. RPC stats completion evidence is copied and
sanitized before OTel; the original RPC error/event and machine status code remain
with the host. Direct host SDK writes and framework error logs are host-owned.

| Channel | Default and explicit policy | Boundary |
|---|---|---|
| GenAI message/tool/retrieval payload | Capture disabled until an explicit payload policy or raw opt-in; bounded JSON/text before and after sanitation | GenAI contract governs these channels; diagnostic policy does not replace it |
| GenAI references, labels and IDs | Declared safe labels or bounded fingerprints according to reference policy | Hashing is not anonymity; host controls original labels and artifact storage |
| GenAI metadata (operation/model/provider/server/purpose) | Declared safe values stay literal; other bounded values are fingerprinted in non-raw spans; metric labels use finite allowlists | Local scope retains originals privately; direct host attributes bypass this route |
| Executor/HTTP panic and RPC completion error text | Neutral diagnostic policy suppresses arbitrary text; explicit sanitizer or raw mode required | Original error/result/panic identity remains with caller; default does not format arbitrary objects |
| Exception message, stacktrace, trace status, slog err/panic | The same processed diagnostic result reaches every library-owned sink | Sanitation precedes fanout; exporter-side masking is not the library privacy mechanism |
| Security reason | Same diagnostic policy; suppressed by default | Action/validator/code/severity are host-supplied bounded machine metadata, not content-scanned text |
| Local Enrich span/log fields | Trusted local typed values copied into context | Host must choose safe values; local logging permission does not imply network permission |
| HTTP URL/path, route and user-agent attributes | Upstream HTTP instrumentation behavior; these ordinary request fields do not pass through diagnostic.Policy | Host must choose safe routes/formatters and review/redact request metadata; default panic/error sanitation does not sanitize every HTTP attribute |
| Inbound baggage and GenAI scope | No imported trust by default; explicit ingress key allowlist | A typed marker or manual OTel baggage is not evidence of trusted local origin |
| GenAI WithScope defaults | Stored privately without span/log/baggage writes; each tracker applies metadata policy at emission | Local trust does not grant raw export; explicit Meta wins over defaults |
| Outbound baggage | No business keys by default; explicit egress allowlist | Trace acceptance/rejection is independent of business baggage permissions |
| Custom attributes, direct SDK calls, custom handlers/exporters | Host-owned | These can bypass all library-controlled content routes |

`diagnostic.NewPolicy` validates immutable configuration. Input strings are valid
UTF-8 and at most 65536 bytes; output is at most 4096 bytes. Positive configured
limits can lower these maxima. Oversized or invalid text is rejected before a
sanitizer receives it. Callback errors/panics and invalid output become bounded
categorical diagnostics; callback error text is never echoed. Explicit raw/custom
mode may call host Error/String formatters before string bounds can be checked;
arbitrary formatter behavior, callback runtime and callback side effects belong
to the host. The library cannot bound execution of arbitrary host code.

Security observation metadata is checked before any span/event writes. Invalid
reason input returns a bounded diagnostic before writes. A sanitizer failure
omits reason content and adds the categorical `metry.diagnostic.status=suppressed`
attribute while retaining the already made host decision. Sampling still applies, including to a child of an unsampled
remote parent; security events are not an audit delivery guarantee.

Propagation is local to a Provider/transport, never a global OTel mutation.
`propagation.NewPolicy` independently configures accepted remote trace context,
ingress baggage keys, egress baggage keys and additional reserved host keys.
Default accepted trace parents preserve normal parent-aware sampling. Explicit
trace rejection does not erase a pre-existing trusted local parent. Detached
trace continuation remains governed by its explicit resume contract.

Trusted local enrichment wins over allowed inbound values. Remote collisions with
`msg`, `time`, `level`, `source`, `trace_id`, `span_id` and configured reserved keys
are rejected. ContextHandler also protects fields attached through WithAttrs;
custom handlers may implement additional behavior under host control. Plain
manual OTel baggage and its type markers cannot create provenance. Local Enrich
is not content sanitation and should receive host-approved values.

Allowlisted ordinary W3C members without `metry.attr.type` are admitted as
strings. An explicitly supplied invalid/empty marker is rejected; an allowlist
is still required. `Enrich` takes raw UTF-8 values: spaces, Unicode, commas and
literal percent escapes survive local spans/logs and permitted wire roundtrips.
Keys remain W3C tokens; invalid keys and invalid UTF-8 values are skipped.

Visible baggage and egress select a lexicographically sorted key prefix after
last-wins deduplication, stopping at the first member that would exceed 64 members
or 8192 encoded bytes (commas and properties included). These are the pinned OTel
v1.47.0 bounds. The SDK/transport may serialize the selected members in any order.
The trusted private ledger keeps omitted values for local spans/logs and later
allowlisted egress. A recording span receives one `metry.baggage.dropped` event
per affected call with only a channel and dropped count; diagnostics contain no
keys, values or SDK error text. With no recording span no event is emitted.
Extraction reports on the original recording span, if present; it never mutates
a remote span. Library-owned overflow does not delegate retention to an unordered
SDK partial result.

Provider SDK composition exposes a wrapped propagator. Map, HTTP and gRPC paths
apply the same local ingress/egress policy. Custom propagator declared Fields are
used coherently for both directions; injection clears only those declared fields
in a reused carrier. A nil carrier/base is a no-op and nil extraction retains the
input context. Custom callbacks deliberately writing other data, mutating globals,
or direct host SDK/exporter writes remain outside this wrapper's guarantee.

There is no authentication, authorization, token cryptography, moderation,
validator execution, durable audit store or framework-specific policy engine.

Host-owned usage/outcome/evaluation mapping and portable consumer gates: [integration mapping](genai/INTEGRATION_MAPPING.md).
