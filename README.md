# VELA: Raw binary codec for Go — hardened prototype (v0.2)

VELA is an **experimental, no-code-generation** Go implementation of a compact
binary codec with raw values, optional shared field dictionaries, borrowed
views, adaptive column batches, encoded projection/filtering, framing, and a
checksummed append-only storage log.

**Status:** the implementation has undergone focused security hardening and local
unit/race/fuzz/benchmark checks. It has **not** received an external security
review or crash-injection certification, so it must not yet be described as
universally production-ready. Official Protobuf, MessagePack, and real gRPC
comparison source is supplied separately in `benchmarks/compare`; it could
not be executed here due to blocked module downloads. No universal
performance advantage is claimed. gRPC is an RPC framework, not a codec.

**Go 1.23+**; core runtime has **zero external dependencies**, CGO or code
generation. The optional external-comparison submodule is isolated.

## Run

```sh
go test ./...
go test ./... -run '^$' -bench . -benchmem -benchtime=500ms
go test ./... -race
go test . -fuzz '^FuzzReadFileEnvelope$' -fuzztime=15s
 go test ./batch -fuzz '^FuzzOpen$' -fuzztime=15s
```

No dependencies beyond the Go standard library. Requires Go >= 1.23.

## Wire format: version 1

One-byte headers: `TTTxxxxx`; three-bit kind (`0..7`) and five-bit inline
length/value (`0..30`), where `31` indicates an absolute unsigned LEB128
varint follows.

| TTT | Meaning |
|:---:|---|
|000|unsigned integer|
|001|ZigZag signed integer|
|010|special: null, false, true, float32, float64|
|011|binary blob|
|100|UTF-8 string|
|101|array count, then values|
|110|map count, then key tokens + values|
|111|reserved (extensions currently unsupported)|

Special constants: `40=null`, `41=false`, `42=true`, `43=float32 LE`,
`44=float64 LE`.

Map keys are unsigned varints: if odd, the key is inline UTF-8 with length
`token>>1`; if even, `token>>1` is a nonzero ID from a shared `Dictionary`.
Key order is not canonical. Map values may be arbitrarily nested up to depth 64.
Raw documents with only inline keys are standalone. Shared-dictionary messages
require out-of-band dictionary negotiation or an independently stored dictionary.

Example wire bytes of `{id:300, age:30, active:true, name:"Asha"}` in field
insertion order (Writer) are:

```text
c4 05 69 64 1f ac 02 07 61 67 65 1e 0d 61 63 74 69 76 65 42 09 6e 61 6d 65 84 41 73 68 61
```

30 bytes raw, 15 bytes with keys `id=1,age=2,active=3,name=4` shared.
The same JSON in Go's default compact string is 47 bytes and handwritten
fixed-schema Protobuf wire is 13 bytes for this example. **DICT excludes the
shared dictionary cost**. These sizes are not benchmark speed results.

### Usage

```go
b, err := vela.Marshal(map[string]any{"id": uint64(300), "name": "Asha"})
if err != nil { panic(err) }
view, err := vela.Open(b)
if err != nil { panic(err) }
id, ok, err := view.GetUint("id")
_ = id; _ = ok; _ = err

w := vela.NewWriter(make([]byte, 0, 128))
w.BeginMap(1)
w.Key("id")
w.Uint(300)
w.EndMap()
raw, err := w.Finish()
_ = raw; _ = err
```

`Open` checks a complete document. `Get` and `Find` return borrowed views;
callers must retain the original backing buffer unchanged. `Unmarshal` creates
owned Go maps/slices/strings and copies byte arrays. Plain `Marshal` supports
maps, slices, primitive types, and optionally uses **reflection only as a
compatibility fallback** for structs and typed containers; writer primitives
do not reflect. Use `UnmarshalInto` to decode into typed Go values:

```go
type Person struct {
    ID   uint64   `vela:"id"`
    Name string   `json:"name"`
    Tags []string `vela:"tags,omitempty"`
}
encoded, err := vela.Marshal(Person{ID: 300, Name: "Asha"})
if err != nil { panic(err) }
var person Person
if err := vela.UnmarshalInto(encoded, &person); err != nil { panic(err) }
```

`vela` field tags take precedence over `json` tags. Both support a field name,
`omitempty`, and `-`. Exported fields without tags use their Go names. Typed
decoding supports pointers, string-keyed maps, slices, arrays, byte arrays,
booleans, strings, integers and floats, with integer overflow checks. Nil
maps and slices encode as null; empty non-nil containers encode as empty.
Types implementing `encoding.TextMarshaler` and `encoding.TextUnmarshaler`
(including `time.Time`) encode as strings. Channels, functions, complex values,
and maps with non-string keys are unsupported. `UnmarshalIntoWithDictionary`
and `UnmarshalIntoWithLimits` provide the corresponding dictionary and limit
variants.

### Atomic file serialization and lazy file views

```go
path := "person.vlf"
v := map[string]any{"id":uint64(300), "name":"Asha"}
err := vela.MarshalFile(path,v)           // private 0600, atomic replace
if err != nil { panic(err) }
owned, err := vela.UnmarshalFile(path)    // returns Go values
var person Person
err = vela.UnmarshalFileInto(path, &person) // decodes a typed .vlf file
view, err := vela.OpenFileView(path,vela.FileOptions{})
name, ok, err := view.GetString("name") // no full object materialization
_, _, _, _ = owned, name, ok, err
```

- `vela.WriteFile(path, value, vela.FileOptions{Dictionary: dict})`: embeds
  the dictionary **in the same self-contained file**. File envelopes use `VLF1`
  framing, version/flags, canonical varints and CRC32C.
- `vela.WriteTo(writer, value, options)` and `vela.ReadFrom(reader, options)`
  support stream sources/sinks and consume one framed file value.
- `vela.OpenFileView` checks framing, checksum and the encoded value then
  permits lazy field access. It owns the file buffer.
- `vela.MarshalLimited(value,dict,limits)` can bound untrusted input *before*
  growing an output buffer; `UnmarshalWithLimits` and `OpenWithLimits` bound
  hostile encoded input. Default maximum encoded input: 64 MiB.
- `batch.WriteFile` and `batch.ReadFile` persist complete self-contained batches
  through the atomic file envelope. `go run ./examples/file` demonstrates all.

Important: file atomicity relies on a same-filesystem rename and supported
filesystem flush semantics; CRC32C provides corruption detection, **not**
authenticity or encryption. See `SECURITY.md` and `WIRE.md`.

### Batch

```go
rows := []map[string]any{
    {"id": uint64(100), "paid": true},
    {"id": uint64(101), "paid": false},
}
packed, _ := batch.Marshal(rows)
page, _ := batch.Open(packed)
matched, _ := page.FilterEq("paid", true) // row indexes
_ = matched
```

It evaluates raw, bitset booleans, frame-of-reference unsigned integers, and
packed dictionary strings, selecting the smallest encoded column in `Marshal`.
`MarshalFast` skips raw fallback work for specialized columns; it reduces
encoding allocations at the cost of possibly larger data. Bit packing uses
byte groups instead of per-bit loops. Batch `OpenWithLimits` rejects excessive
row/column/cell counts, malformed varints and invalid dictionary indexes. Batch pages
are self-contained; column names are stored once. The current implementation
spends additional CPU at encode time comparing candidate encodings and does not
maintain a secondary index for predicates. This tradeoff can lose benchmarks.

### Storage

`storage.Open(path)` opens an append-only CRC32C log. `Put`, `Get`, `Delete`,
`FilterEq`, `Compact`, and `Snapshot` remain available. Hardening includes
**exclusive process locks** on an adjacent `.lock` file, streaming recovery,
truncation of incomplete tail entries, rejection of CRC corruption, live-data
budget enforcement, and bounds on record and log size. `Compact` atomically
renames the compacted log and swaps the active descriptor after rename. Data
is synchronized on each successful write; see `SECURITY.md` for remaining
failure modes.

This is still an **in-memory indexed, single-writer log**, not a transactionally
isolated/multi-node database with WAL rotation or secondary indexes.

## Limitations and known tradeoffs

* Schema-less keys cost bytes versus numeric field IDs in fixed-schema Protobuf.
* Reused dictionaries require negotiation or persistence. No on-wire handshake
  is implemented.
* Raw view lookup is O(number of keys); no skip index for unknown field paths.
* `Marshal` using arbitrary Go structs is reflection-backed and can be slower
  than generated code. `Writer` avoids reflection but requires explicit fields.
* Mixed-type batch columns fall back to individual encoded values.
* Encoded predicates currently support equality; no range index or SQL parser.
* A single-process lock and fsync per mutation limit storage write throughput.
* Framing is minimal (version and length only), with no authentication.
* No code generation, unsafe package, assembly, compression library or CGO.
* **This implementation has not been independently audited or subjected to a
  multi-platform power-loss/crash-consistency test campaign.**

## Fair benchmarks

Benchmark competing payload formats on the *same semantic data* and distinguish:
raw bytes, framing overhead, `ns/op`, `B/op`, `allocs/op`, batch size, partial
read, end-to-end RPC latency, and throughput at each payload scale. See
`benchmark_test.go` and `benchmarks.md`. `BenchmarkProtoWireFixedSchema` is a
manually written **wire-format** encoder, not a measure of official Protobuf
or gRPC libraries. An end-to-end gRPC comparison requires a real gRPC server,
equivalent client/server behavior and installed dependencies.

## Safety

Untrusted bytes must be bounded by transport length limits (default example
frame maximum: 16 MiB). Codec depth is bounded to 64, but memory/CPU budgets
and adversarial nested document tests should be strengthened. Stored checksums
are for accidental corruption, not authentication. No zero-copy ownership of
mutable input is safe across concurrent mutation of that buffer.

## Results

See [benchmarks.md](benchmarks.md) for measured results and limitations. The
small 4-field map serialized to 30 B in RAW versus 47 B JSON; 1,024 repeated
records compressed to 1,847 B TILE versus 45,910 B JSON. These are sample-
specific outcomes, not universal advantages. Official gRPC was **not** run.

## Comparative benchmark submodule

Official Protobuf + MessagePack and local TCP gRPC benchmark harnesses live in
`benchmarks/compare`. They are **not** part of `go test ./...` so that the core
module continues to compile without third-party modules. On a machine with
Internet access run:

```sh
cd benchmarks/compare
go mod tidy
go test -run TestPayloadSizes -v .
go test -run '^$' -bench 'Benchmark(Encode|Decode|GRPCLocalhost)' -benchmem -count=5 .
```

Protobuf uses official `proto.Marshal` with a `dynamicpb` runtime descriptor
(no generated code). Note that the much faster generated Protobuf Go code
has not been benchmarked. `GRPCLocalhost` starts a real localhost TCP gRPC
server, not a fake service. Consult `benchmarks-hardened.md` for **executed**
core benchmark results and limitations.
