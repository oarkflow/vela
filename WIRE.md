# VELA wire layout v1

## Naked raw payload (schema-less)

`tag = (kind << 5) | small`, `small = 0..30` inline. A low five-bit
value of `31` must be followed by **canonical** unsigned LEB128 with actual
size/value >=31. Signed integers use ZigZag. Floats are IEEE754 little endian.
UTF-8 is mandatory for strings and map keys. `0x40` null, `0x41` false,
`0x42` true, `0x43` float32 and `0x44` float64. Bit-kind 111 is reserved;
unknown extensions are rejected.

| Kind | Contents |
|---|---|
| 0 | unsigned integer 0..uint64 max |
| 1 | zigzag encoded int64 |
| 2 | null/bool/floats (special header) |
| 3 | byte length, raw bytes |
| 4 | UTF-8 byte length, bytes |
| 5 | count, values recursively |
| 6 | map count, repeated key-token and value |

Map keys have a separate canonical uvarint token. An **odd** token means
UTF-8 key of `(token>>1)` bytes follows. An **even** nonzero token means
external/embedded dictionary ID `(token>>1)`; IDs start at one. Dictionary
messages are not self-contained unless the enclosing file includes the
serialized dictionary. The first occurrence of duplicate keys wins in both
materialized and borrowed access; applications requiring canonical semantics
must implement a unique-key policy above the codec.

## File envelope

```text
magic:      'VLF1'                    (4 bytes)
flags:      0x00 or 0x01              (1 byte)
if flag 1:  dictionary length          (canonical uvarint)
            dictionary bytes          (size as declared)
payloadLen: length of naked raw value (canonical uvarint)
payload:    VELA raw value             (payloadLen bytes)
checksum:   CRC32C(flags..payload)    (4-byte little endian)
```

Embedded dictionary bytes: `keyCount uvarint` then repeated `keyByteLength
uvarint | UTF-8 key`, with keys stored in numeric ID order. Duplicate names
are rejected. File reader checks CRC and full payload, then rejects trailing
bytes, except `ReadFrom` which consumes one envelope on a stream.

## Batch VLB1

```text
magic:    'VLB1' (4 bytes)
rows:     uvarint
columns:  uvarint
for each column:
  nameLen: uvarint
  name:    UTF-8 name
  mode:    1 byte (0 raw, 1 bool, 2 uint-pack, 3 string-dict)
  size:    uvarint
  payload: size bytes
```

- Raw column: concatenation of `rows` VELA raw values.
- Bool: presence bitmap and boolean bitmap, each `(rows+7)/8` bytes.
- Unsigned bit-pack: minimum unsigned uvarint, one byte bit width (0..64),
  packed unsigned differences in LSB-first bit order.
- String dictionary: dictionary count and UTF-8 entries, one byte bit width,
  followed by packed IDs (0 = null, 1..count = dictionary entry).
- Missing column keys and explicit nil are represented identically as null.
- `batch.WriteFile` wraps an entire VLB1 batch as a VELA byte-array value
  inside a `VLF1` envelope, adding CRC and atomic file persistence.
