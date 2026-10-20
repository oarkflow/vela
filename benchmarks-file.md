# `.vlf` versus JSON file operations

The benchmarks in `file_benchmark_test.go` compare the same four-field Go
record in `.vlf` and JSON files. Writes in both cases marshal the record,
create a temporary file, set private permissions, sync the file, rename it,
and sync the parent directory. Reads reopen a file on each iteration.
The field benchmark validates the `.vlf` envelope and payload before lookup;
the JSON path reads and unmarshals the complete document into a one-field
struct. Both return the same `id` value.

Run with:

```bash
go test -run '^$' -bench 'BenchmarkFile(Write|ReadStruct|ReadField)$' -benchtime=200ms -count=2
```

Observed on Apple M2 Pro, Go 1.27.0, macOS (two runs, rounded):

| Complete operation | `.vlf` | JSON |
|---|---:|---:|
| Durable file write | 20.8–25.1 ms | 19.5–20.7 ms |
| File read into struct | 17.1–23.9 µs | 21.5–22.0 µs |
| File read and `id` lookup | 16.1–18.6 µs | 18.3–23.5 µs |

These measurements do not meet a 10× advantage for any of the three complete
file operations. File creation and synchronization dominate the write case;
opening and reading files dominate the two read cases. The figures are
workload-specific and vary with filesystem cache and host load. Codec-only
and already-open-view benchmarks answer different questions and should not be
used as substitutes for end-to-end file measurements.
