# Performance/memory implementation report

Baseline: `215af3ee33bcc4b58f0b532da1b1958c3990bb8a`. Final candidate is the commit containing this report; its SHA is recorded in the external acceptance records. Go 1.27.1, Darwin arm64, Apple M1 Max. See environment.json for commands and all raw logs in this directory.

String reads the immutable internal ledger by value, Values still makes a defensive snapshot, and only missing private scope fields trigger fallback. Histogram, Counter and Gauge convert labels directly into owned attributes. Removed the unused copyLabels helper and stale README reference. Public API, telemetry policies, dependencies and cardinality behavior are unchanged.

## Gates

All 56 initial benchmark cases have five baseline and five candidate samples. 41 unstable or apparently regressing cases received five alternating 500ms pairs. One case required 10 additional 1s pairs; all 15 samples remain in the final calculation. CPU uses ratio of median ns/op and the fixed 1.10 threshold. The corrected comparator passes every path. Worst paired CPU ratio: 1.058868.

Complete private scope (final paired candidate median; all zero B/op and allocs/op):

| Unrelated ledger values | Median ns/op | B/op | allocs/op |
|---:|---:|---:|---:|
| 0 | 12.41 | 0 | 0 |
| 8 | 12.27 | 0 | 0 |
| 64 | 12.31 | 0 | 0 |
| 256 | 12.4 | 0 | 0 |

String and partial fallback are zero-allocation on every measured size. Direct conversion is zero allocations for empty labels and one for 8/64/256. Every zero-value metric is zero B/op and zero allocs/op. For full Add/Record on both noop and ManualReader-backed SDK, B/op and allocs/op never regress;64/256 labels reduce allocations from 9 to 6. Example Counter→noop allocation medians:

| Labels | Baseline B/op | Candidate B/op | Baseline allocs/op | Candidate allocs/op |
|---:|---:|---:|---:|---:|
| 64 | 24400 | 19496 | 9 | 6 |
| 256 | 92240 | 73768 | 9 | 6 |

See comparison.md and paired-results.json for all measured paths.64/256 labels are stress inputs, not dimension recommendations. Bytes measure transient allocations, not retained heap or a leak.

## Tests and independent acceptance

All mandatory final root, nested grpc/otlp normal/race, and boundary e2e checks PASS (EXIT_CODE=0). Mandatory final suite execution is recorded in final-commands.json/final-root-*.log; nested grpc/otlp normal/race logs record local checks. OTLP uses integration-tag loopback receivers. Boundary e2e tests additionally cover scope, metric registry and propagation. Source SHA256 values and identical baseline/candidate benchmark fixtures are in source-manifest.json.

The frozen 25-point acceptance contract is requirements.md. Reviewer A must supply a complete source/test/evidence matrix with 25/25 PASS and 100%/APPROVE. Reviewer B independently checks correctness and must APPROVE with no found unfixed errors. Their final records name the same candidate SHA and are stored separately after commit creation. This report alone does not substitute for either verdict. Absence of found errors is not proof that all possible errors are absent.

## Scope boundary

Context/log/propagation copy paths were inspected, as recorded in hot-path-inspection.md. No additional changes were justified by dedicated benchmarks, so those ownership/privacy-sensitive paths retain their snapshots. No global/unbounded cache, consumer changes, release publication or external-provider measurements were introduced.
