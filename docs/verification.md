# Repository verification

The infrastructure follows ragy commit `91e3ff2cdc87c49b5ad9d1cf0afa23d35832521e`.
Make calls Go and golangci-lint directly. Modules are discovered from `go.mod`,
excluding hidden directories and vendor. Every command uses `GOWORK=off`.
All discovered modules are tested and published, including `integration/otlp`.
There is no separate release inventory, aggregate runner or generated configuration.

| Command | Scope |
|---|---|
| `make modules` | Sorted module inventory. |
| `make lint` | Config verification, formatting diff and lint; no source changes. |
| `make test` | Fresh ordinary tests with the race detector in every module. |
| `make test-integration` | `integration` tag and `TestIntegration` prefix. |
| `make test-e2e` | `e2e` tag and `TestE2E` prefix. |
| `make test-live` | `live` tag and `TestLive` prefix; external services or published artifacts. |
| `make fix` | Go fixes, formatting and automatic lint fixes; modifies files. |
| `make cover` | Per-module `coverage.out` and coverage report. |
| `make bench` | Benchmarks with allocation statistics. |
| `make fuzz` | Every discovered fuzz target separately, 30 seconds per target. |

Both the build tag and function prefix are required for profile tests. A module
without tests for a profile executes none; the repository's integration and e2e
profiles contain real scenarios. Tool failures propagate immediately.

Integration covers real loopback OTLP HTTP and gRPC transports, GenAI SDK
composition, runnable examples and candidate artifacts. Candidate tests construct
standard Go module ZIPs from current package inputs, prepare internal manifests,
and resolve them in fresh temporary consumers with no replacements. Semantic
observation mapping, gRPC and OTLP fixtures run against these artifacts. Downloaded ZIPs in the current Go cache may serve as a local mirror before the
public proxy; consumer module caches remain fresh. Third-party modules retain
public checksum verification; only unpublished candidate module
paths are exempted. This validates source artifacts, not a production publication.

E2E covers producer/worker trace snapshot flows, propagation, async feedback,
evaluations, GenAI scope and metrics. Ordinary tests retain lower-level validation,
concurrency, privacy and mapping contracts. `scripts/` contains Go Make/release
regressions in the existing root module, not an executable runner.

Live includes a public-module consumer baseline pinned to `v0.10.0` by default.
`METRY_PUBLISHED_VERSION` explicitly selects another version. This baseline is
historical evidence, not a gate for the current candidate; v0.10.0 did not publish
the OTLP fixture module. No paid provider test is currently implemented.

Prerequisites are Bash, Git, Make, Go, golangci-lint, a C toolchain for race tests,
loopback networking and public Go module proxy/checksum access. Missing selected
prerequisites fail tests; no prerequisite-based skip is used. No PDF runtime,
Python, observability credentials or external collector is required. Docker is
needed only to reproduce the Linux checks from macOS.

CI pins Go 1.27.2 and golangci-lint 2.14.0, downloads dependencies for every discovered
module, caches all module sums and runs lint, unit, integration and e2e sequentially.
Make uses PATH tools; it does not install or validate tool versions. CI checks main,
main pull requests and root `v*` tags with read-only permissions and no persisted
Git credentials.

Release regressions use disposable local bare repositories and isolate user Git
configuration; only file transport is permitted. Run another Bash implementation
with `METRY_TEST_BASH=/bin/bash GOWORK=off go test -race -count=1 -run '^TestRelease' ./scripts`.
Performance measurements, fuzz campaigns and live tests remain separate. Historical
reports keep their original commands. See [release runbook](release/runbook.md).
