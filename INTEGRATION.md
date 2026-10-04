# OTel integration contract

`metry.New(ctx, opts...)` creates and owns its SDK pipeline. `WithExporter` accepts
`sdktrace.SpanExporter`; `WithMetricExporter` accepts `sdkmetric.Exporter`;
`WithSampler` accepts `sdktrace.Sampler`. There are no opaque exporter/sampler
wrappers. Exporters supplied to a successful constructor transfer to its SDK
pipeline and are shut down with it. Invalid configuration does not transfer
ownership. Caller-created exporters must be cleaned up by the caller if creation
fails. Omitted exporters select a documented noop pipeline.

`metry.FromSDK(SDKDependencies{TracerProvider, MeterProvider, Propagator})`
borrows dependencies. All three dependencies are required and must be non-nil,
including interfaces containing typed nils. It does not install globals, change
sampling, own resource attributes, close providers or start workers. The host
owns their lifecycle and configures exporters/sampling through the upstream SDK.
`Shutdown` and `ForceFlush` only operate on owned resources. To explicitly flush
borrowed resources, call `FlushDependencies(ctx)`; providers implementing
`ForceFlush(context.Context) error` are called, others are skipped. There is no
implicit ownership transfer for injected providers.

`Provider.SDK()` returns a detached `SDKDependencies` value for transport
instrumentation. The dependencies are references, not copies of SDK providers.
Calling their methods directly is a host-owned integration boundary. A nil or
zero Provider exposes noop providers and the default W3C propagator for optional
instrumentation; this is not a constructor for an owned pipeline.

New validates service name, finite trace ratio in [0,1], nil options, and explicitly
supplied nil exporters/sampler before SDK construction. Successful shutdown is
idempotent and uses a ten-second upper timeout, respecting an earlier caller
deadline. An explicit custom sampler is used as supplied. The default inherits a
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
