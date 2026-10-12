# Implementation evidence

`requirements.md` freezes the 25 acceptance obligations before implementation. `hot-path-inspection.md` records the scope boundary. `environment.json` and each log record commands, baseline, Go/OS/arch and exits. Benchmark source lives permanently in root `metrics_performance_test.go` and `genai/performance_test.go`.

## Reproduce

From the repository root, create an isolated baseline without touching this checkout:

```sh
mkdir -p /tmp/metry-performance-baseline
git archive 215af3ee33bcc4b58f0b532da1b1958c3990bb8a -o /tmp/metry-performance-baseline.tar
tar -xf /tmp/metry-performance-baseline.tar -C /tmp/metry-performance-baseline
cp metrics_performance_test.go /tmp/metry-performance-baseline/metrics_performance_test.go
cp genai/performance_test.go /tmp/metry-performance-baseline/genai/performance_test.go
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/run.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/paired.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/extended-paired.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/final-tests.py
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/implementation/compare.py
```

`run.py` serializes tests/race and benchmark processes and uses GOWORK=off with writable GOCACHE. Both revisions use identical benchmark fixture source and Go1.27.1. Benchmark fixtures and single-series warmup precede ResetTimer; every Add/Record and conversion remains in the measured loop. The reader is a real ManualReader backed by a local SDK MeterProvider, not an external endpoint. 64/256 labels are scaling stress. Bytes/allocations measure temporary allocation, not retained heap or leaks.

Initial suite logs precede final legacy-helper/test cleanup. `final-root-tests.log` and `final-root-race.log` supersede their initial counterparts and verify the final source. The nested suites were built after that cleanup. Paired CPU repeats, when needed, alternate baseline/candidate in isolation; their commands and raw logs accompany the comparison. No benchmark runs concurrently with compilation/race or another task benchmark.

The candidate commit contains source, permanent tests/benchmarks and this evidence. Candidate SHA is recorded externally in acceptance outputs to avoid a self-referential commit hash. Reviewers inspect the same committed tree and evidence. Reviewer outputs are acceptance records, not implementation changes.

## Noise handling and corrected gate aggregation

The initial run showed wide ns/op dispersion. Every case with >20% max/min sample range in either revision, plus every median CPU regression >10%, was repeated with five alternating baseline/candidate pairs at500ms (41 paths). Final CPU decisions use median candidate ns/op divided by median baseline ns/op. Median of pairwise ratios is diagnostic only. An initial draft incorrectly used the latter; both reviewers identified this evidence defect before acceptance. `paired-initial-results.json` preserves that draft data. The only failing case after corrected aggregation, memory/histogram/64, received ten additional alternating pairs at1s. The final gate retains all15 samples, rather than selecting a favorable subset. Initial raw logs, initial comparison, all repeated raw logs and selection rules remain saved.

`comparison.md` reports initial medians beside final decision ratios; `paired-results.json` contains all final repeated samples. `compare.py` enforces the exact56 expected benchmark paths, five samples per initial revision, completion of every selected paired repeat, allocation budgets and the1.10 CPU threshold. Full suite commands and source hashes are recorded in `final-commands.json` and `source-manifest.json`. Root boundary e2e checks are additional local coverage for scope/metric/propagation integration.
