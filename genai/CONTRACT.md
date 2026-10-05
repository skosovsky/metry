# Effective instrumentation contract

`NewContract(options...)` validates the same runtime configuration as
`NewTrackerFromProvider` without registering SDK instruments. `Tracker.Contract()`
returns the settings actually installed in that tracker. The zero contract is
invalid. The schema identity is `metry.genai.semconv.e07f4eb.v1`.

`RawEnabled()` reports permitted raw content capture. `ReferenceRawEnabled()`
reports raw reference permission independently, including when content capture is
disabled. The contract exposes detached profile and channel declarations. It contains no
payloads, sanitizer callbacks or credentials. `PayloadChannels()` resolves each
channel's output byte limit and declares fixed input byte, depth and entry bounds.
JSON channels use the context budget; evaluation reasoning uses the event budget,
whether it is carried by a span attribute or a declared event. Text channels use
UTF-8 wire strings. Defaults are 65536 context bytes and 4096 event bytes. Explicit
context budgets must range from the minimum `{"truncated":true}` envelope through
1048576 bytes; explicit event budgets must range from 1 through 1048576 bytes.

`CheckConformance(contract, declarations, fixtures...)` rejects invalid composition
before executing fixtures. Fixtures derive `PayloadEvidence{Attribute, Value}`
from actual controlled exports. Controlled text on events additionally belongs in
`ObservedEvent.Payloads`. Every observed controlled field requires one named
payload witness; duplicates, missing witnesses, incompatible channels and values
above the effective output budget fail. JSON shape, enum, unit, ownership,
cardinality and accounting checks remain independent of the byte checks.

Expected scenario facts stay separate from exported observations. Counter sums
cannot prove write counts: fixtures need independent write witnesses when those
counts matter. Report diagnostics contain categorical results rather than raw
values or fixture error messages. A passing report establishes conformance of the
covered composition and scenarios; it does not certify arbitrary host exporters
or prove absence of PII in all host telemetry.

There is no compatibility path for the former profile/raw-flag checker arguments,
JSON-only payload evidence or `metry.genai.presence` profile identity.

JSON numbers are decoded with `UseNumber` and retain their numeric spelling,
including integers beyond float64 precision and decimal/exponent notation.
Upstream array channels retain a complete-element prefix with separate truncation
metadata. Tool extensions may use an envelope, which counts toward depth/node
bounds; retained numeric values are never rounded. Custom policies
receive the original `RawMessage` bytes after bounded input validation; their own
transformations remain host responsibility, while metry bounds their output before
encoding.

Hash policy canonicalizes object ordering and JSON whitespace, then fingerprints
numeric lexemes without converting them to floating point. Adjacent integers above
2^53 remain distinguishable. Equivalent spellings such as `1`, `1.0` and `1e0` may
have different fingerprints; fingerprints are not a proof against hash collisions
or anonymity. Redaction intentionally replaces values rather than retaining them.

Metric assertions now take `metrytest.MetricSelector` in place of a bare name.
Use the complete exported instrumentation scope to select a known scope;
version, schema URL and attributes also participate in exact scope matching. A nil
scope requires one unique name across all scopes. Typed helpers verify aggregation;
duplicate candidates fail before conversion. Attribute helpers likewise require
exact OTel types, including when expected values are zero, false or empty.
