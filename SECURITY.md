# VELA v0.2 — security and deployment status

## What is verified

- Truncated/overflow/noncanonical varints are rejected for raw, batch, and file decoding.
- Hard bounds on decoded byte size, nesting depth, node count, container size,
  individual byte/string size, map-key length, and batch cells prevent common
  allocation-amplification and CPU-amplification cases.
- Raw file envelopes use CRC32C, explicit version/flags, and strict maximum
  lengths, with atomic same-directory temporary-file writes and sync before
  rename. `UnmarshalFile` rejects trailing bytes.
- Dictionary mode on disk embeds keys and validates the dictionary before data.
- Batch validates offsets, lengths, bit widths, unsigned overflow, UTF-8,
  repeated dictionary keys, and packed dictionary reference indices.
- Store's exclusive lock uses `flock` (Linux/macOS). Recovery reads bounded log
  entries incrementally rather than reading the entire log. Tail writes are
  truncated only when incomplete, not on detected checksum corruption.
- Go unit tests, race detector, `go vet`, file-envelope and batch fuzz tests,
  and a Darwin/arm64 cross-build were run for this revision.

## Not guaranteed: production readiness requires these additional steps

1. **External security review** of all wire-format decoders and storage paths.
2. **Power-loss/crash consistency testing** with injected errors, different
   Linux/macOS filesystems and unreliable storage devices.
3. **Cryptographic authentication and encryption** at the transport/application
   boundary where required. CRC32C is not an integrity MAC.
4. **Limits tuned by workload**, including the total live dataset, CPU budget,
   number of concurrent readers, number of concurrent requests, network timeouts,
   and maximum RPC message sizes.
5. **Version negotiation**, formal compatibility policy, migration tests,
   canonical serialization/hashing, and schema evolution if external peers need them.
6. **Backup/restore, durability, monitoring, and incident-response procedures**.
   There is no replication, MVCC, multi-key atomic transaction or secondary index.
7. **Benchmarking on actual workloads**, including official Protobuf generated
   messages and gRPC paths (not run here due blocked module registry).

## Operational cautions

- The `View.Bytes`/`StringBytes` APIs borrow input bytes. Do not mutate or reuse
  the backing buffer until the view is no longer used. View sharing across
  goroutines is read-only only; dictionary lookups are concurrently safe.
- Schema-less map keys are not canonical. Duplicate keys are interpreted
  using the **first occurrence** in both lazy views and the materializing
  decoder. Do not sign or hash uncanonicalized raw maps for security claims.
- `Append`/`Marshal` and the raw direct `Writer` are intended for trusted Go
  input. For untrusted data use `MarshalLimited`/`AppendLimited`, and always
  decode input with appropriate limits.
- File writes replace the path atomically on local filesystems supporting
  rename; they do not protect against malicious directory replacement or
  concurrent external writers. Do not store data in attacker-writable dirs.
- The store uses Unix `flock` and a persistent `.lock` file; it is not suitable
  for concurrent network filesystem access. Cross-platform Windows locking
  has not been implemented.
- Persistent store keeps all live data in RAM and syncs per mutation, limiting
  scale and write throughput. Memory use has not been bounded for metadata
  separately from record payload bytes.
