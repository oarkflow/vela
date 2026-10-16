# ARCHIVED v0.1 benchmark report — see `benchmarks-hardened.md` for the latest tested version.

# VELA benchmark report — 16 October 2026

## Environment

Go 1.23.2, Linux/amd64 container, AMD EPYC 9V74 (Go benchmark `-5`), run
with `go test ./... -run '^$' -bench . -benchmem -benchtime=500ms -count=1`.
Machines and run conditions can change these figures. Raw logs are available
in `benchmarks.txt`. These figures are *measurements*, not guarantees.

## Single-object 4-field maps

Payload: `id:300, age:30, active:true, name:"Asha"`.

| Operation | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Standard-library JSON marshal (map) | 1,018 | 336 | 10 |
| VELA raw marshal (map) | **156.9** | **64** | **1** |
| VELA shared dictionary marshal | 181.8 | 64 | 1 |
| Standard-library JSON unmarshal (map) | 895.7 | 568 | 15 |
| VELA raw unmarshal (map) | **339.7** | **384** | **9** |
| VELA validated borrowed Open | 66.05 | 0 | 0 |
| VELA field lookup after Open | 30.24 | 0 | 0 |
| VELA raw reusable Writer | 70.72 | 0 | 0 |
| Manually written **schema-bound** Protobuf wire encoder | **3.512** | 0 | 0 |

Note: `Get` runs on an already validated `View`, so the 30.24 ns number is
not end-to-end decoding. `BenchmarkProtoWireFixedSchema` is **not** an
implementation of Google's official Protobuf library and is deliberately a
schema-bound lower bound. Comparing it to a generic map serializer is not a
fair codec winner determination. Its result illustrates how much performance
might still be available to optimized fixed-layout implementations.

**Wire sizes:** JSON 47 bytes; VELA raw 30 bytes; VELA shared dictionary 15
bytes **excluding dictionary distribution costs**; Protobuf wire 13 bytes
with pre-agreed field numbers/types.

## 1,024 three-field records

Records have sequential `id`, boolean `active`, repeated string `status`.

| Representation | Byte size |
|---|---:|
| Standard JSON array | 45,910 |
| 1,024 independent VELA records | 29,013 |
| VELA TILE adaptive batch | **1,847** |

| Operation | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| JSON marshal entire batch | 637,101 | 278,617 | 7,170 |
| VELA adaptive TILE marshal | 216,349 | 122,712 | 32 |
| VELA Fast TILE marshal | **196,138** | **98,138** | **27** |
| VELA independent ROW marshal × 1,024 | **148,198** | **65,536** | 1,024 |
| JSON filter after full unmarshal from bytes | ~1,088,475* | 496,954 | 13,328 |
| VELA Open batch + encoded status equality filter | **5,681*** | **5,632** | **12** |
| VELA filter on previously opened page | 5,051 | 4,864 | 1 |

`*` Filtering comparison comes from a separate 500 ms run; both start with
encoded bytes. It is still workload-specific: VELA has direct access to a
single packed column, whereas JSON requires materializing objects. A JSON
system with its own persisted index or column store is a different baseline.

**Honest interpretation:** VELA achieves smaller batches and beats JSON on
these examples. Its own independent-row encoder is currently faster than TILE
encoding, but resulting TILE pages are far smaller and faster for selective
status scans. Do not claim it is universally faster than JSON, protobuf, gRPC,
or specialized compressed columns based on these limited tests.

## Why no official gRPC/Protobuf benchmark results?

The environment cannot resolve `proxy.golang.org`, and no Protobuf/gRPC
modules are installed. Accordingly, a genuine `google.golang.org/protobuf`
serializer benchmark or a `google.golang.org/grpc` client-server benchmark
**was not run**. The handwritten wire demonstration is not a substitute.

When dependencies can be installed, compare:

1. `encoding/json` map and typed struct; custom fast JSON baselines.
2. Official `proto.Marshal` / `proto.Unmarshal` on an agreed message schema;
   use `dynamicpb` if strict no-code-generation is required.
3. Official gRPC over in-process `bufconn` and TCP (separate measurements);
   and VELA using the same request/reply semantics and framing.
4. FlatBuffers / FlexBuffers, CBOR, and MessagePack with analogous ownership,
   projection, and batch behavior.
5. Real datasets with skewed keys, sparse fields, nested objects, long strings,
   already-compressed payloads, and random-access reads.
6. Benchmark `-count=10`, statistical significance, `-cpu 1,4`, p95/p99 under
   load, and memory lifetime across repeated requests.

A formal release would require compatibility fixtures, extended fuzzing,
fault injection, secure resource limits, and external code review.
