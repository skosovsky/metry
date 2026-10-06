# OTel integration contract

`metry.New(ctx, opts...)` creates and owns its SDK pipeline. `WithExporter` accepts
`sdktrace.SpanExporter`; `WithMetricExporter` accepts `sdkmetric.Exporter`;
`WithSampler` accepts `sdktrace.Sampler`. There are no opaque exporter/sampler
wrappers. Exporters supplied to a successful constructor transfer to its SDK
pipeline and are shut down with it. Invalid configuration does not transfer
ownership. Caller-created exporters must be cleaned up by the caller if creation
fails. Omitted exporters select a documented noop pipeline.

`metry.FromSDK(SDKDependencies{TracerProvider, MeterProvider, Propagator}, opts...)`
borrows dependencies. All three dependencies are required and must be non-nil,
including interfaces containing typed nils. It does not install globals, change
sampling, own resource attributes, close providers or start workers. The host
owns their lifecycle and configures exporters/sampling through the upstream SDK.
`Shutdown` and `ForceFlush` only operate on owned resources. To explicitly flush
borrowed resources, call `FlushDependencies(ctx)`; providers implementing
`ForceFlush(context.Context) error` are called, others are skipped. There is no
implicit ownership transfer for injected providers.

`Provider.SDK()` returns a detached `SDKDependencies` value for transport
instrumentation. Tracer/meter dependencies are references, not copies of SDK providers.
The propagator is wrapped by the provider-local trust policy: default trace-only,
no inbound business trust or outbound business baggage. Configure New through
`WithPropagationPolicy`; borrowed providers through `WithSDKPropagationPolicy`.
`WithDiagnosticPolicy` / `WithSDKDiagnosticPolicy` configure library-owned text.
See [privacy boundaries](PRIVACY.md). Direct tracer/meter writes and the original
host propagator remain host-owned integration boundaries. A nil or
zero Provider exposes noop providers and a secure W3C propagation wrapper for optional
instrumentation; this is not a constructor for an owned pipeline.

New validates service name, finite trace ratio in [0,1], nil options, and explicitly
supplied nil exporters/sampler before SDK construction. Shutdown is terminal and idempotent, including failure. Concurrent callers
wait for the first call and receive the same first result. Its context controls
a ten-second upper timeout, respecting an earlier deadline. An already cancelled
first call cannot be retried with a live context; supply a fresh live context for
shutdown after request cancellation. Provider owns lifecycle state and must not
be copied after first use. An explicit custom sampler is used as supplied. The default inherits a
valid parent's sampling decision and applies the ratio only to new roots. Links
do not count as parents. GenAI keep sampling is an explicit head-sampler opt-in.

Executor instruments belong to each Provider and are created once under concurrent
registration. Different providers do not share a process-global instrument cache.
Request/response types remain caller-owned. Generic metric names and label values
must be static or otherwise bounded by the host; GenAI's vocabulary and series
budget do not automatically cover the generic registry or executor.

HTTP and generic executor use root `metry.HTTPHandler` and `metry.ExecutorWrap`.
gRPC implementation and dependencies belong exclusively to `middleware/grpc`,
which uses the explicit SDK bridge. Core imports neither gRPC nor its OTel adapter.

TraceSnapshot and AsyncHandle tokens are bounded to 2048 encoded bytes and carry
trace references only, not baggage or business state. Supported tracestate is
W3C-compatible and limited to 512 serialized bytes. Oversized input is rejected
before decode; diagnostics do not contain token contents. Tokens are unsigned;
the host controls trust, persistence and delivery.

## Concurrency and method lifetime

Publish a successfully constructed Provider/Tracker once. The standard OTel SDK
supports concurrent instrumentation; custom injected dependencies must provide
the same required concurrency behavior. Do not copy Provider, Tracker or handles
after first use. Stop and join recording work before shutting down the SDK. Metry
does not reopen a terminal owned pipeline or guarantee delivery for new calls
after shutdown. Borrowed SDK lifetime remains entirely with the host.

| Object | Supported use | Caller responsibility |
|---|---|---|
| Provider | Concurrent instrumentation/registration; concurrent Shutdown waits for the first result | Fresh live shutdown context; do not race host disposal with recording |
| Tracker, Recorder, EvaluationRecorder | Shared app-scoped instrumentation; immutable configuration and synchronized dimension accounting | Custom payload policies/classifiers/adapters must support concurrent calls |
| Operation/Tool/Retrieval/Invocation handles | Finish/Close and streaming validation serialize on the local handle mutex | Ordinary invalid results can be corrected; valid finalization or callback panic is terminal |
| LinkedSpanWriter | Copies share synchronized state during one telemetry callback | Join worker goroutines before callback returns; late writes are inert; first non-nil SetError wins, callback return error has priority |
| TraceSnapshot, AsyncHandle, contract/profile snapshots | Detached value snapshots/references | Tokens are unsigned; host owns trust and durable delivery |

Pass non-nil contexts. DTOs and their slices/maps are consumed during recording;
do not mutate them concurrently with the call. Installed host callbacks and their
state remain host-owned; profile copies do not make those objects immutable.
`ObservationError` is a bounded telemetry diagnostic, not a wrapped business
error. Keep the original result/error for business handling. `ErrFinalized` means
the local recording is terminal; it proves neither successful export nor business
success. See [outcomes and callback failure](genai/OUTCOMES.md).

## Nil, zero and explicit noop

`NewTrackerFromProvider(nil)` and `NewRecorderFromProvider(nil)` return an error.
Choose `NoopRecorder()` explicitly for optional GenAI telemetry; it validates
supported recording DTO contracts while dropping exports, without running custom
payload/classifier callbacks. `NoopEvaluationRecorder()` validates ready results
and drops exports. Nil `Tracker.Recorder()` selects noop; nil `Tracker.Contract()`
returns an invalid zero contract. Do not infer nil safety for other direct
Tracker methods: for example, `Profile` and `RecordInteraction` require a valid
constructed Tracker. A nil/zero Tracker is not a general noop substitute.

Nil/zero `Provider.SDK()` exposes noop dependencies and secure default propagation;
`Shutdown`/`ForceFlush` are no-ops without owned SDK resources. This does not make
all methods nullable: `Provider.StartSpan` returns `ErrNilTracerProvider` without
a configured tracer, and `HTTPHandler(nil, ...)` panics. Constructors still reject
missing/typed-nil explicit SDK dependencies. Nil/zero recording handles report a
bounded missing-recording error. Disabled `NoopAsyncHandle()` is distinct from an
invalid zero handle: it contains no identity and cannot serialize; linked emission
drops it, while evaluation requires a real trace reference.

## Evidence scope

The loopback OTLP consumer in `integration/otlp` exercises real SDK exporters and
serialized protobuf names through the public API. A transport-name assertion
alone does not establish numeric values, status, privacy or accounting.

`TestPublicAPI_OTLPGenAISemantics` adds receiver-byte checks for the owned resource,
GenAI scope/version/schema, known-zero vs absent usage, histogram counts/sums,
currency/cost, a child parent and detached link, failed outcome and absence of a
raw sentinel in serialized trace/metric requests. This compact fixture is an
external public consumer and uses real OTLP exporters, not in-memory test SDKs.
OTLP sums alone cannot prove how many counter Add calls occurred, and this fixture
does not cover arbitrary host attributes or downstream collector transformations.
GenAI export-oracle/conformance fixtures cover their declared scenarios; neither layer
certifies arbitrary host attributes, custom exporters or external collector
transformations. [CONTRACT.md](genai/CONTRACT.md) defines independent expected
facts and exported witnesses for schema `metry.genai.semconv.e07f4eb.v1`.

## Clear-break migration

| Previous API/behavior | Replacement |
|---|---|
| Opaque `SpanExporter`, `MetricExporter`, `TraceSampler` | SDK exporter/sampler interfaces at composition root |
| `metrytest.MetrySpanExporter` / `MetryMetricExporter` | Pass SDK exporters directly |
| Test-only dependency wiring | `metry.FromSDK` for production; partial test doubles remain test-only |
| Root `GRPC*` functions | Corresponding `middleware/grpc` functions |
| `middleware/http.Handler` | `metry.HTTPHandler` |
| `middleware/executor.Wrap` and options | `metry.ExecutorWrap` and root options |
| Ratio used for every child | Parent inheritance; ratio for roots |
| Borrowed SDK pointers shut down incidentally | Borrowed lifecycle belongs to host; explicit flush available |

No deprecated aliases, legacy token-format branch, or compatibility package is
provided. Tokens retain their JSON shape; HTML escaping is disabled before base64
encoding so every supported W3C tracestate fits the corrected bounds.
