# JSON versus VELA HTTP request/response

The benchmark uses the `/echo` example with the same typed message and the
same reply for all three formats. The client encodes a request, sends it through
an HTTP handler, then decodes the response. Each handler reads a bounded body,
decodes the message, prefixes `received: ` to `text`, encodes the reply, and
writes HTTP status, content type, content length, and body. The body limit is
1 MiB. JSON uses `encoding/json`; VELA uses this package's HTTP and codec APIs.

| Format | Content-Type | Request body | Response body |
|---|---|---|---|
| JSON | `application/json` | 47 bytes | 57 bytes |
| VELA binary | `application/vnd.vela` | 33 bytes | 43 bytes |
| VELA text | `application/vnd.vela+text` | 42 bytes | 52 bytes |

The actual JSON request and response are:

```json
{"id":42,"text":"hello","tags":["demo","http"]}
{"id":42,"text":"received: hello","tags":["demo","http"]}
```

The equivalent human-readable VELA bodies are:

```text
{id=u42;tags=["demo";"http"];text="hello"}
{id=u42;tags=["demo";"http"];text="received: hello"}
```

The binary body represents the same fields, but is intended to be sent as
bytes, not pasted into a terminal. Neither HTTP format includes the `.vlf`
file envelope. The sizes above are body sizes; HTTP headers and transport
framing are excluded.

Observed on Apple M2 Pro, Go 1.27.0, macOS, with three runs per format:

| Complete exchange | JSON | VELA binary | VELA text |
|---|---:|---:|---:|
| In-process handler, no socket | 3.10–3.16 µs | 3.32–3.33 µs | 5.76–5.97 µs |
| In-process allocations | 42/op | 71/op | 125/op |
| Localhost HTTP socket | 53.5–58.0 µs | 64.8–84.2 µs | 66.3–97.3 µs |
| Localhost allocations | 105/op | 134/op | 188/op |

For this small message, binary VELA uses 14 fewer request bytes and 14 fewer
response bytes than JSON. JSON was faster and used fewer allocations in these
runs. The localhost timings varied substantially between runs, including an
earlier run in which all three formats measured roughly 52–58 µs. The stable
conclusion is that **this implementation does not show a 10× HTTP speedup**.
VELA text is readable and smaller than JSON here, but took more CPU and
allocations. Results depend on payload, machine, Go version, transport, and
concurrency.

Run the comparison from the repository root:

```bash
go test ./examples/http -run TestHTTPComparisonPayloads -v
go test ./examples/http -run '^$' -bench '^BenchmarkHTTPInProcess$' -benchmem -benchtime=500ms -count=3
go test ./examples/http -run '^$' -bench '^BenchmarkHTTPLoopback$' -benchmem -benchtime=300ms -count=3
```

The localhost benchmark skips if binding a `127.0.0.1` port is unavailable.
Its timing includes HTTP client/server work and loopback transport; the
in-process benchmark excludes sockets. The benchmark source is
[`examples/http/http_benchmark_test.go`](examples/http/http_benchmark_test.go).
