# VELA v0.2 benchmark and validation report

**Status:** Executed core comparisons against standard-library JSON only. Official
Protobuf, MessagePack, and real localhost gRPC benchmark code has been added
under `benchmarks/compare`, but **it was not executed in this container**;
external Go modules cannot be downloaded (DNS/egress blocked). Do not use the
numbers in this report to claim superiority over those libraries or gRPC.

## Environment and method

- Linux/amd64, Go 1.23.2, AMD EPYC 9V74 (shared CI/container CPU).
- `go test ./... -run '^$' -bench 'Benchmark(...)' -benchmem -benchtime=300ms -count=3`
- Medians of three runs (not statistical confidence intervals); shared-host
  noise is visible in the raw data.
- `benchmarks-hardened-final.txt` includes all three measurements per case.
- Four-field raw map: `id=300`, `age=30`, `active=true`, `name="Asha"`.
- Batch: 1,024 maps with sequential IDs, boolean active, and three statuses.

## Encode/decode, individual map

| Operation | JSON median ns/op | VELA median ns/op | Relative speed |
|---|---:|---:|---:|
| Marshal map | 811.0 | 153.7 | VELA 5.28× faster |
| Unmarshal map | 976.4 | 425.5 | VELA 2.29× faster |

| Other VELA path | median ns/op | bytes allocated | allocs/op |
|---|---:|---:|---:|
| Validate + borrowed view (`Open`) | 115.7 | 0 | 0 |
| Borrowed field lookup (`GetID`) | 85.36 | 0 | 0 |
| Reused manual raw writer | 78.90 | 0 | 0 |

### Encoded bytes

| Format | Bytes | Notes |
|---|---:|---|
| Standard JSON | 47 | Map payload |
| VELA standalone RAW | 30 | Self-describing keys |
| VELA shared DICT | 15 | **Dictionary overhead excluded** |
| Handwritten Protobuf wire | 13 | **Not** official `proto.Marshal` benchmark; field schema hardcoded |

## Batch 1,024 records

| Operation | Median ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| JSON batch marshal | 674,183 | ~278,893 | 7,170 |
| VELA `MarshalFast` | 174,602 | 6,944 | 12 |
| VELA RAW encode each record | 147,756 | 65,536 | 1,024 |
| JSON unmarshal + filter | 1,115,229 | ~496,954 | 13,328 |
| VELA open TILE + filter | 6,616 | 5,632 | 12 |

- **3.86×** faster batch encode than standard JSON on this sample.
- **~169×** faster open-and-filter than JSON full materialization on this
  sample. This is not a comparison with indexed DBs or columnar query engines.
- **18% slower** batch encoding than independent VELA RAW record encoding,
  despite much lower allocation and substantially better size. The next
  optimization should address this remaining encode CPU gap.

| Payload | Size |
|---|---:|
| JSON array | 45,910 B |
| VELA individually encoded RAW values (sum) | 29,013 B |
| VELA TILE/`MarshalFast` | **1,847 B** |

VELA TILE used **~24.9× fewer bytes** than JSON on this repeated-field dataset.
It is not a general compression ratio and would be lower for random strings,
large sparse values or heterogeneous data.

## Executed validation

- `go test ./... -count=1`: **PASS**.
- `go test -race ./...`: **PASS**.
- `go vet ./...`: **PASS**.
- `GOOS=darwin GOARCH=arm64 go build ./...`: **PASS (cross-build only)**.
- File-envelope fuzzing: **109,137 executions**, no failures in the short run.
- Batch-open fuzzing: **191,289 executions**, no failures in the short run.
- File round-trips with/without dictionaries, checksums, truncated content,
  oversized file rejection, batch disk I/O, exclusive locks, streaming
  recovery and bit-pack widths 0..64: **PASS**.

These checks do not prove crash safety or freedom from security vulnerabilities.

## Official comparisons included, but not run here

From a connected development environment:

```bash
cd benchmarks/compare
go mod tidy
go test -run TestPayloadSizes -v .
go test -run '^$' -bench 'Benchmark(Encode|Decode|GRPCLocalhost)' -benchmem -count 5 .
```

Dependencies: `google.golang.org/protobuf`, `google.golang.org/grpc`,
`github.com/vmihailenco/msgpack/v5`. Protobuf uses a runtime `dynamicpb`
descriptor to respect the no-generated-code requirement. This is still **not**
a measure of how VELA compares with optimized generated Protobuf codecs. The
gRPC suite starts a real TCP listener on 127.0.0.1 and measures two server
pipelines, including protobuf-wrapped VELA bytes. gRPC is an RPC transport
framework; there is no one-number "VELA vs gRPC codec" comparison.

## Remaining release gates

External security audit; chaos/power-loss storage tests; TLS/auth integration;
formal protocol compatibility policy; payload corpus representative of real
workloads; reproducible official benchmark runs on multiple machines;
benchmark optimized Protobuf generated implementations separately; test under
concurrent RPC load and at p50/p95/p99, not just `ns/op`.
