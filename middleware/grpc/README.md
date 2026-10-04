# metry gRPC integration

Install the core library and the gRPC integration separately:

```bash
go get github.com/skosovsky/metry
go get github.com/skosovsky/metry/middleware/grpc
```

Use matching versions of both modules after the clear break. gRPC and otelgrpc
are dependencies of this module only; the root module does not import them.

Create runtime with `metry.New(...)` or borrow a host pipeline with `metry.FromSDK(...)`,
then pass `provider` directly. The implementation obtains dependencies through
`provider.SDK()` and never installs global providers:

- `metrygrpc.ServerOptions(provider)`
- `metrygrpc.ClientDialOption(provider)`
