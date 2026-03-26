# metry gRPC integration

Install the core library and the gRPC integration separately:

```bash
go get github.com/skosovsky/metry
go get github.com/skosovsky/metry/middleware/grpc
```

Use matching versions of both modules (for example, `v0.3.x` with `v0.3.x`).

Create runtime with `metry.New(...)`, then pass `provider` directly:

- `metrygrpc.ServerOptions(provider)`
- `metrygrpc.ClientDialOption(provider)`
