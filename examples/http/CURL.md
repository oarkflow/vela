# Send and receive human-readable VELA with curl

Start the HTTP server from the repository root:

```bash
go run ./examples/http -serve
```

In another terminal, send a **VELA text** value and receive a VELA text
response. No JSON or binary file is involved:

```bash
curl -i -X POST http://127.0.0.1:8080/echo \
  -H 'Content-Type: application/vnd.vela+text' \
  -H 'Accept: application/vnd.vela+text' \
  --data-binary '{id=u42;text="hello"}'
```

The response body is:

```text
{id=u42;text="received: hello"}
```

`{...}` is a VELA map, `u42` is an unsigned integer, and `"hello"` is
a UTF-8 string. `s"hello"` is also accepted. `;` separates map entries.
This syntax also supports signed integers (`i-3`), floats (`f1.5`),
datetimes (`t"2026-10-16T01:02:03Z"`), durations (`d"1h30m"`),
`true`/`false`, null, byte arrays (`b64"AAE="`), arrays (`[u1;"two"]`),
and nested maps. See
[WIRE.md](../../WIRE.md) for the complete v1 text grammar.

The compact binary HTTP format remains available with
`Content-Type: application/vnd.vela`. The `.vlf` file envelope is a third
representation with a `VLF1` header and checksum.
