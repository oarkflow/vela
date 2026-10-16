# Optional official Protobuf, MessagePack and localhost gRPC benchmarks

Run from this subdirectory (requires internet for dependencies at first run):

```bash
cd benchmarks/compare
go mod tidy
go test -run TestPayloadSizes -v .
go test -run '^$' -bench 'Benchmark(Encode|Decode|GRPCLocalhost)' -benchmem -count 5 .
```

Requires the published official `google.golang.org/protobuf`, official
`google.golang.org/grpc` and `github.com/vmihailenco/msgpack/v5` packages.

- Protobuf uses official `proto.Marshal`, `proto.Unmarshal` and a *runtime*
  `dynamicpb` schema, not generated Go code. Production generated message
  implementations can be significantly faster.
- MessagePack uses its public Marshal and Unmarshal map APIs.
- Codec measurements use the same four scalar fields; schemas, numeric
  representations and decoder destination types vary by design.
- gRPC tests use a REAL `127.0.0.1` TCP listener and grpc-go Unary RPC calls,
  no mocks. One path transports dynamic protobuf Person messages, and another
  protobuf BytesValue containing VELA. This measures different RPC pipelines;
  neither is a valid universal verdict on gRPC versus VELA.
- The core VELA module has no third-party dependencies. This is a separate
  module so offline builds of the codec still work.
