# Supported GenAI semantics

Metry implements a checked subset of the OpenTelemetry GenAI Development registry
pinned to [e07f4ebacb08f56db8c4c882d117720333fbca04](https://github.com/open-telemetry/semantic-conventions-genai/tree/e07f4ebacb08f56db8c4c882d117720333fbca04).
The GenAI schema is `https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev`;
its standard semantic-conventions dependency is v1.44.0. Development definitions
can change. This pin describes the supported exported subset, not all agent
frameworks or every upstream instrument.

Schema/Contract identity is `metry.genai.semconv.e07f4eb.v1`. GenAI tracer and
meter scope are `metry/genai`, version `semconv-genai-e07f4eb+metry-v1`, with the
pinned GenAI schema URL. Resource schema URL remains independently host-owned.
Vendored fixture sources, license and hashes support offline export-oracle tests.
Host SDK attributes and arbitrary exporter transformations are outside this contract.

## Boundaries

| Observation | Canonical name | Kind | Meaning |
|---|---|---|---|
| Real model interval | operation + protected request model, when present | CLIENT remote; INTERNAL local | Explicit `ExecutionLocality`; never inferred from provider |
| Completed model/stream | same | INTERNAL | `metry.observation.kind=completed`; timestamps are recording time |
| Tool interval | `execute_tool` + protected tool name | INTERNAL | Single host tool call |
| Retrieval interval | `retrieval` + protected source, when present | CLIENT remote; INTERNAL local | Local retrieval is an explicit extension |
| Agent interval | `invoke_agent` + protected name | CLIENT remote; INTERNAL local | Remote hosted or local agent invocation |
| Workflow interval | `invoke_workflow` + protected name | INTERNAL | Application-owned workflow interval |
| Ready evaluation | `evaluation` | INTERNAL linked root | Completed-result extension; event is `gen_ai.evaluation.result` |
| Feedback/async | protected extension observation name | INTERNAL | No inference, execution-store or export guarantee |

The `evaluation` operation value and linked completed root are explicit metry
extensions permitted by the registry's open operation vocabulary; the result event
and its score/name/explanation fields use upstream definitions.

Logical/operation/attempt identity changes resource accounting, never locality
or span kind. Logical summaries do not add model duration, usage or cost; actual
attempts account their own known final facts once. Invocation spans never aggregate
child token usage or model durations. Host code owns scheduling, retries, tool
execution, retrieval, grading, handoffs, checkpoint state and delivery.

## Controlled fields

U means the pinned GenAI registry; S means its standard dependency; E means an
explicit metry extension. Types describe OTel attribute values; JSON is a string
attribute with a checked structured value.

| Controlled keys | Type | Classification and scope |
|---|---|---|
| `gen_ai.provider.name`, `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.response.model` | string | U; explicit provider/request/observed-response facts |
| `server.address`, `server.port`, `error.type` | string, int, string | S; bounded endpoint and error taxonomy |
| `exception.type`, `exception.message`, `exception.stacktrace` | string | S exception event; diagnostic privacy applies |
| `gen_ai.usage.input_tokens`, `output_tokens`, `cache_write.input_tokens`, `cache_read.input_tokens`, `reasoning.output_tokens` | int | U; inclusive totals and optional components; known zero preserved |
| `gen_ai.system_instructions`, `gen_ai.input.messages`, `gen_ai.output.messages` | JSON array string | U; schema-valid arrays after whole-element truncation |
| `gen_ai.tool.name`, `gen_ai.tool.call.id` | string | U; protected finite machine name and span-only call reference |
| `metry.gen_ai.tool.call.arguments`, `metry.gen_ai.tool.call.result` | JSON string | E; any JSON value, while upstream standalone tool schemas require objects |
| `metry.gen_ai.tool.error` | bool | E outcome auxiliary field |
| `gen_ai.evaluation.name`, `score.value`, `score.label`, `explanation` | string, double, string, string | U ready-result vocabulary; no universal score range |
| `gen_ai.data_source.id`, `gen_ai.retrieval.query.text`, `gen_ai.retrieval.top_k`, `gen_ai.retrieval.documents` | string, string, int, JSON array string | U; references protected, query opt-in, optional provenance |
| `metry.gen_ai.retrieval.name` | string | E optional caller label; never replaces canonical span name |
| `metry.gen_ai.retrieval.returned_documents` | int | E host returned count; no inferred correspondence with provenance list |
| document object `id`, `score`; `metry.document.version`, `metry.document.digest` | string, double; string, string | U id/score; E opaque version/digest properties |
| `gen_ai.agent.name`, `gen_ai.agent.version`, `gen_ai.agent.id`, `gen_ai.workflow.name`, `gen_ai.conversation.id` | string | U; agent.id only stable hosted resource identity, conversation existing caller identity |
| `metry.gen_ai.agent.id`, `metry.gen_ai.workflow.id`, `metry.gen_ai.workflow.version` | string | E local agent/workflow references |
| `metry.gen_ai.execution.locality`, `metry.gen_ai.invocation.kind`, `metry.gen_ai.operation.status` | string | E fixed locality, invocation-kind and outcome enums |
| `metry.gen_ai.operation.purpose`, `metry.gen_ai.usage.cost`, `metry.gen_ai.cost.currency` | string, double, string | E host purpose/cost/currency; no pricing calculation |
| `metry.gen_ai.usage.audio_seconds`, `image_count`, `video.seconds`, `video.frames` | double, int, double, int | E observed media usage, separate from token components |
| `metry.gen_ai.sampling.keep`, `metry.gen_ai.cache.hit`, `metry.gen_ai.embedding.model` | bool, bool, string | E sampling hint/cache/auxiliary embedding metadata |
| `metry.gen_ai.measurement.<field>.source`, `.unit` | string | E explicit provenance/unit; fields input_tokens/output_tokens/cache_write_input_tokens/cache_read_input_tokens/reasoning_output_tokens/cost/audio_seconds/image_count/video_seconds/video_frames |
| `metry.gen_ai.stream.calculation` | string | E fixed formula identity for derived streaming metrics |
| `metry.gen_ai.token.component`, `metry.usage.source` | string | E component enum and measured/provider/estimated metric provenance |
| `metry.usage.mapping`, `metry.usage.price_reference` | string | E inclusive mapping and external price reference |
| `metry.causal.run_id`, `parent_run_id`, `operation_id`, `step_id`, `attempt_id`, `kind`, `prompt_identity`, `predecessor` | string | E caller-owned span-only causal identity/evidence |
| `metry.observation.kind`, `metry.dimension.overflow` | string, bool | E completed observation and cardinality overflow evidence |
| `metry.evaluation.status`, `observation_id`, `experiment_id`, `dataset_reference`, `case_id`, `trial_id`, `target_reference`, `grader_reference`, `artifact_reference` | string | E result identity, optional complete experiment tuple and independent target/grader provenance; no dataset/artifact retrieval or grader execution |
| `metry.diagnostic.field`, `metry.diagnostic.rule` | string | E bounded rejected-observation diagnostic |
| `metry.gen_ai.payload.<channel>.truncated`, `metry.gen_ai.retrieval.documents.truncated` | bool | E separate truncation evidence for array channels |

Abbreviated suffix lists in this table retain the first key's namespace. The
executable `Schema().Fields()` catalogue supplies exact names/types and required
contract declarations. `Schema().Events()` covers standard `exception`, upstream
`gen_ai.evaluation.result` and extension `metry.observation.rejected` event fields.

## Instruments

| Name | Instrument / unit | Classification and accounting |
|---|---|---|
| `gen_ai.client.inference.operation.input_tokens` / `output_tokens` | Int64Histogram / `{token}` | U final inclusive totals, one known-value observation |
| `metry.gen_ai.client.token.component.usage` | Int64Histogram / `{token}` | E optional components; never sum into totals |
| `gen_ai.client.operation.duration` | Float64Histogram / `s` | U actual remote model intervals only |
| `metry.gen_ai.operation.duration` | Float64Histogram / `s` | E local intervals and completed model observations |
| `gen_ai.execute_tool.duration` | Float64Histogram / `s` | U single tool interval; canonical tool/status/error dimensions |
| `metry.gen_ai.invocation.duration` | Float64Histogram / `s` | E agent/workflow intervals; fixed kind/locality/status dimensions only |
| `metry.gen_ai.cost` | Float64Counter / currency dimension | E explicit host cost, no cross-currency sums |
| `metry.gen_ai.client.ttft` | Float64Histogram / `s` | E first-token observation, not upstream first-chunk |
| `metry.gen_ai.client.derived.output_tokens_per_second` | Float64Histogram / `{token}/s` | E output_tokens/(total−TTFT) |
| `metry.gen_ai.client.derived.mean_output_token_interval` | Float64Histogram / `s` | E (total−TTFT)/(output_tokens−1), requires >1 output token |
| `metry.gen_ai.client.video.seconds` / `video.frames` | Float64Histogram / `s`; Int64Histogram / `{frame}` | E host video usage |

### Metric dimensions

The base model set **B** contains required `gen_ai.provider.name`,
`gen_ai.operation.name` and optional `gen_ai.request.model`,
`gen_ai.response.model`, `server.address`, `server.port`. Optional dimensions are
emitted only when those facts exist. All string dimensions use finite allowlists;
unlisted values become `unknown`, unlisted integer ports become 0. Provider
absence uses explicit bounded `unknown`, not inferred backend identity.

| Instruments | Normal dimension keys | Dimensions retained on overflow |
|---|---|---|
| Input/output token histograms | B + `metry.usage.source` | source |
| Token component histogram | B + `metry.usage.source` + `metry.gen_ai.token.component` | source + component |
| Remote and local/completed duration histograms | B + `metry.gen_ai.operation.status` + optional `error.type` | status |
| Tool duration histogram | `gen_ai.tool.name` + `metry.gen_ai.operation.status` + optional `error.type` | status |
| Invocation duration histogram | `metry.gen_ai.invocation.kind` + `metry.gen_ai.execution.locality` + `metry.gen_ai.operation.status` | all three fixed enums |
| Cost counter | B + `metry.gen_ai.operation.purpose` + `metry.gen_ai.cost.currency` + `metry.usage.source` | currency + source |
| TTFT histogram | B | none |
| Derived output-token rate / mean interval histograms | B + `metry.usage.source` + `metry.gen_ai.stream.calculation` | source + calculation |
| Video seconds / frames histograms | B + `metry.usage.source` | source |

Every overflow row also emits `metry.dimension.overflow=true` and discards the
other keys. This keeps bounded semantics such as known provenance, currency,
component and outcome distinct instead of merging them into misleading totals.
Invocation's three fixed enums have nine natural combinations; names or business
references never participate. Normal series and reserved overflow combinations
share the configured per-instrument budget.

Calculation values are `output_tokens_over_generation_window` and
`generation_window_over_output_token_intervals`, respectively. Component values
are `input_cache_write`, `input_cache_read`, `output_reasoning`; provenance values
are `measured`, `provider_reported`, `estimated`. Sources represent the producer's
actual evidence, not confidence inferred by metry.

Derived streaming instruments require known output tokens and a positive window;
they carry output-token provenance and calculation identity. They cannot show
actual gaps, stalls or first output chunk. Invocation metrics contain no names,
provider, IDs, versions, conversations or document references. All other metric
labels use finite profile allowlists and per-instrument series budgets; cost
currency is explicit. No references/IDs become metric labels.

## Content and privacy

`ContentPart.Content` serializes `content`; `ContentPart.Response` serializes
`response`; tool responses use type `tool_call_response`. The obsolete message
finish-reason field is removed. Recognized text/tool vocabulary has independent
checks because upstream GenericPart permits extension objects. Existing media
references are generic parts, not a claim of upstream URI-part encoding. Metry
bounds and sanitizes references and never fetches media.

Array channels retain complete top-level elements and emit separate truncation
markers. Tool extension envelopes remain valid JSON; exact number spelling and
large integer distinctions are preserved. Default reference protection applies
when content capture is disabled, including retrieval provenance and invocation
IDs. `Contract.RawEnabled()` describes content capture permission; `Contract.ReferenceRawEnabled()` reports reference raw
permission independently. Raw or custom callbacks do not certify anonymity.
See [CONTRACT.md](CONTRACT.md), [MIGRATION.md](MIGRATION.md) and root [PRIVACY.md](../PRIVACY.md).
