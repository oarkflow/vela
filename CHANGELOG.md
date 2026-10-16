# Changelog

## v0.2 — Hardening and file I/O (2026-10-16)

- Added VLF1 file envelope with CRC32C, self-contained dictionary storage,
  atomic file replacement, stream reader/writer and lazy file views.
- Added batch file persistence through VLF1.
- Added configurable byte, node, key, depth, container, row, column and cell
  limits with hostile-input validation and bounded encoder preflight.
- Optimized bit packing and eliminated per-value intermediate buffers in the
  fast batch encoder. Optimized packed-field validation without omitting checks.
- Eliminated heap allocation in common `Open` and lazy field lookups.
- Added scalar query predicates directly on borrowed encoded bytes.
- Streaming log recovery; process lock, bounded live state and log, partial
  append repair, and safer compaction handle transitions.
- Added fuzz corpus seeds, malformed file/batch tests, race tests, examples,
  and an *optional* official Protobuf/MessagePack/localhost gRPC benchmark suite.
- Not yet independently audited, power-loss certified, or benchmarked against
  third-party library implementations in this offline environment.
