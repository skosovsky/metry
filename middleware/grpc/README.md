# metry gRPC integration

Install the core library and the gRPC integration separately:

```bash
go get github.com/skosovsky/metry
go get github.com/skosovsky/metry/middleware/grpc
```

Use `metrygrpc.ServerOptions()` on the server and `metrygrpc.ClientDialOption()` on the client after calling `metry.Init(...)` in your application.
