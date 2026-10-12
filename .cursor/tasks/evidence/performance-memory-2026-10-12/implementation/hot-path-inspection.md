# Context/log copy inspection

Inspected `internal/enrichment/context.go`, `genai/scope_context.go`, `enrich.go`, `loghandler.go`, `propagation/policy.go`, `labels.go`, `metrics_registry.go` using `rg 'enrichment.Values|enrichment.String|copyLabels|labelsTo'` and source reads.

`String` is the pure lookup fixed here. `Put` must copy to preserve ancestor immutability. `Values` remains the snapshot API. `slogAttrsFromBaggage` takes a snapshot and then builds a second, owned representation; `ContextHandler.Handle` also builds collision keys and log attributes. `enrichBaggage` snapshots before building bounded transport baggage; propagation similarly snapshots during policy-controlled imports/exports. These are analogous read copies, but remain unchanged: no independent quantitative benchmark and semantic regression justification for expanding implementation scope. Log privacy/collision, enrichment provenance and propagation coverage remains in the full root/race suites. No claim of zero allocations on these paths is made.

The priority implementation has no cache, no shared mutable buffers and no new exported API. Attribute values are immutable strings returned by value. Map iteration order already was unspecified; filtering is unchanged. Metric SDK options, value forwarding, bounds, units, timestamps, cardinality and exporter configuration are untouched.

Removed the now-unused private `copyLabels` helper and its redundant helper-specific tests. Public `LabelsOf` still makes a defensive filtered map; the permanent `copy_control` benchmark uses that equivalent copying implementation. The README no longer mentions the removed helper. Baseline and candidate benchmark source files are identical.
