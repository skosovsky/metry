# Selected upstream fixtures

The files under `model/` and `LICENSE` are unmodified copies from
[OpenTelemetry GenAI semantic conventions, commit e07f4ebacb08f56db8c4c882d117720333fbca04](https://github.com/open-telemetry/semantic-conventions-genai/tree/e07f4ebacb08f56db8c4c882d117720333fbca04).
`PIN.json` records each SHA-256 digest, the Development schema URL and the
independent standard semantic conventions dependency. No network access is
required by these tests.

`pinned_oracle_test.go` validates actual SDK exports against literal names,
kinds, key types, instrument units and these JSON schemas. Recognized text,
tool-call and tool-response parts also use the corresponding `$defs` branch:
the upstream GenericPart alternative otherwise permits malformed known types.
Mutation fixtures cover names, kinds, keys, attribute types, metric units and
aggregation types, as well as malformed JSON structures. The tests do not use
metry Profile declarations as their upstream oracle.

Standalone tool payloads accept arbitrary JSON in metry extension attributes.
They deliberately do not claim the object-only upstream tool schemas. The
object-only schemas are included to make this difference executable.

The schema validator is the test-only
[`github.com/santhosh-tekuri/jsonschema/v6` v6.0.3](https://github.com/santhosh-tekuri/jsonschema/tree/v6.0.3).
