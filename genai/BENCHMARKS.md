# Payload benchmark baseline

Measured on 2026-10-05 with Go 1.27.1, darwin/arm64, Apple M1 Max.
Command: `go test -p 2 -run '^$' -bench '^BenchmarkPayload' -benchtime=100ms -benchmem ./genai`.
Inputs are fixed in `payload_benchmark_test.go`: normal JSON is 64 bytes; boundary
JSON is 65550 bytes, above the default 65536-byte output budget.

| Operation | Input | ns/op | B/op | allocs/op |
|---|---|---:|---:|---:|
| Encode | normal | 1981 | 1379 | 24 |
| Encode | boundary | 785268 | 634454 | 61 |
| Hash sanitation | normal | 2850 | 1789 | 36 |
| Hash sanitation | boundary | 531693 | 557026 | 42 |
| Recording, payloads disabled | normal | 4192 | 6424 | 20 |
| Recording, payloads disabled | boundary | 4398 | 6424 | 20 |
| Recording, raw payloads enabled | normal | 25445 | 17726 | 204 |
| Recording, raw payloads enabled | boundary | 2756818 | 2455314 | 291 |

Recording cases include local SDK span recording; setup is outside the timer,
and there is no network exporter. Disabled refers to payload capture. This single
short baseline is reproducible evidence, not a throughput guarantee or CI
performance threshold. Repeated runs depend on machine load and SDK batching.

`make fuzz FUZZTIME=2s` executed all four targets independently:
`FuzzParseAsyncHandle`, `FuzzParseTraceSnapshot`, `FuzzBoundedJSON`, and
`FuzzPayloadSanitizer`. The smoke run passed, as did the complete race-test,
lint and example gates. These are historical command options. The current `make fuzz` uses the standard
30-second campaign per discovered target; longer campaigns use explicit `go test`
arguments. See [verification](../docs/verification.md).
