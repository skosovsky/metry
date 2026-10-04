// Package genai records application-owned model, tool, retrieval and evaluation
// observations through an app-scoped provider. It owns instrumentation,
// validation and controlled export, not execution, retries, scheduling, pricing,
// artifact storage or business decisions. Host adapters translate their own types
// into these DTOs without a dependency from this package to consumer types.
//
// StartOperation encloses local I/O and measures duration using a monotonic clock.
// Its handle commits final usage once; invalid input may be corrected before a
// successful Finish. ToolHandle and RetrievalHandle follow the same once-only
// contract. Close commits unknown without fabricated results. Explicit Outcome
// success is required; absent response metadata is never copied from the request.
// Stateless adapters return ResponseObservation facts from the actual response.
// Completed-result helpers are observation spans, not execution
// intervals. Logical summaries do not account physical resource usage again.
// Causal IDs and links are optional, caller-owned and never metric dimensions.
// Opaque continuation tokens contain trace references, not business identities.
//
// Measurement zero values mean absence. Present zero is a known observation with
// explicit source and unit. Inclusive totals are not summed with overlapping
// components; absent totals are not inferred. Cost requires an explicit declared
// currency. ObservationError and ErrInvalidObservation identify bounded invalid
// observations without including payload data. Telemetry diagnostics must be
// handled separately from the host's business result or error.
//
// Profile is a snapshotted declaration of supported schema, ownership, encoding,
// units, enum values, payload channels and bounded metric dimensions. Conformance
// is a fixture tool, not a runtime registry or execution engine. Built-in payload
// policy declarations must match the installed policy; raw export requires host
// permission. Policy applies before controlled fan-out. Custom SDK attributes and
// exporter callbacks remain outside that guarantee. Fingerprints are not anonymity.
//
// EvaluationRecorder accepts completed results, independently of target sampling.
// An optional valid reference creates a linked root, never mutates a finished
// span, and does not force sampling. Experiment association is optional but must
// be complete when used. Metry neither runs graders nor deduplicates delivery
// across processes. Noop and sampling drop telemetry without owning execution.
package genai
