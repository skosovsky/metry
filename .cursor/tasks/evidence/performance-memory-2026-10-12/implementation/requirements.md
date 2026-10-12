# Frozen acceptance contract

Baseline: 215af3ee33bcc4b58f0b532da1b1958c3990bb8a. All 25 requirements below are mandatory; denominator and thresholds cannot be changed after measurement. PASS requires implemented source/test or executed evidence, never a plan. A = 100 * PASS / 25 and APPROVE only at 100%. B independently checks correctness on the identical candidate tree.

1. Internal trusted read-only string accessor never exposes mutable ledger storage; no heap allocation on reads.
2. Values remains a detached snapshot; Put cannot mutate ancestors or retain incoming mutable slices.
3. Concurrent read-only String/Values/Put on shared ancestors remains race safe.
4. Complete private scope does no enrichment fallback reads.
5. Partial scope only fills missing fields; local/private precedence is preserved.
6. Only trusted local or explicitly admitted remote strings supply fallback; typed non-string and arbitrary baggage are ignored.
7. Complete scope with 0/8/64/256 unrelated values: 0 B/op, 0 allocs/op, no linear CPU growth.
8. Trusted String and partial fallback: zero heap allocations/read, correct results.
9. All three domain recording paths remove intermediate Labels maps.
10. Conversion filters empty keys/values and attributes remain detached after caller mutation.
11. Conversion with 8/64/256 nonempty labels <=1 alloc/op; empty 0.
12. Histogram/Counter/Gauge zero-value paths remain 0 B/op and 0 allocs/op at 0/8/64/256 labels.
13. Full Add/Record on noop and manual in-memory reader measured at all four sizes, no B/op or allocs/op regression and reductions at 64/256.
14. Comparable measured paths do not regress median ns/op >10%, minimum five samples on identical environment; unstable cases need paired repeats.
15. Metric values, timestamps, units and overflow semantics unchanged.
16. Cardinality budget32 retains exactly29 combinations after 10k distinct declared values; reserved overflow remains bounded.
17. Permanent meaningful AAA regression tests and benchmarks; fixture creation outside timing, invocation costs remain inside timing.
18. Context/log paths inspected for analogous copies; changes outside priority paths require their own benchmarks and semantic regressions.
19. No global/unbounded cache, dependency/consumer/exporter/sampling/payload/business-data changes, nor public API changes.
20. Root go test ./... PASS, including privacy/propagation/scope/metrics/shutdown contracts.
21. Root go test -race ./... PASS.
22. middleware/grpc local suite and race PASS.
23. integration/otlp local integration-tag suite and race PASS without external endpoints.
24. Self-contained evidence saves baseline and candidate identity, Go/OS/arch, reproducible commands, raw logs and measured comparison.
25. Independent A and B final verdicts on identical candidate; A 100%/APPROVE and B APPROVE with no found unfixed errors. Verification is not proof that all possible bugs are absent.

## Internal contract, before implementation

String reads the immutable private ledger directly and returns string values by value. The private accessor's slice must remain confined to this package and never be mutated after publication. Values copies the ledger; Put copies before editing. Complete ScopeFromContext returns immediately; missing fields alone invoke String. Direct labelsToAttributes creates owned attributes and preserves filtering; concurrent mutation of the caller map is unsupported. No further hot-path changes are planned: enrichment/log/propagation snapshot consumers will be inspected and documented.
