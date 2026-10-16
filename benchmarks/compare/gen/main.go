// Command gen writes the sample VELA (.vlf) files in testdata/samples.
// These files are committed fixtures used by the compare benchmarks and tests
// for marshal/unmarshal work; regenerate with: go run ./gen
//
// The samples escalate from a single scalar value to deeply nested documents,
// repeated rows, shared-dictionary files, and adversarial-ish edge shapes
// (deep nesting, long strings, many keys, binary blobs).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/oarkflow/vela"
)

const outDir = "testdata/samples"

type sample struct {
	name  string
	value any
	dict  *vela.Dictionary // nil writes a plain file
}

func samples() []sample {
	// 01 — simplest possible standalone value.
	// 02 — README canonical person record.
	// 03 — full primitive coverage: default inline lengths, uvarint escape at
	//      >=31, ZigZag negatives, float32/64, bools, null, empties, blob.
	// 04 — moderate nesting: maps inside maps, arrays of records, unicode.
	// 05 — repeated rows; exercises the shapes the batch encoder likes.
	// 06 — same rows as 05 but with an embedded shared field dictionary.
	// 07 — deep nesting near MaxDepth, one long string, more than 31 map keys
	//      and 31 array elements, nested arrays, binary blob, byte-heavy text.
	person := map[string]any{"id": uint64(300), "age": uint64(30), "active": true, "name": "Asha"}

	scoped := func(r int) map[string]any {
		return map[string]any{
			"id":       uint64(100 + r),
			"ref":      uint64(9000 + r*7),
			"name":     fmt.Sprintf("worker-%03d", r),
			"role":     []string{"ingest", "transform", "load"}[r%3],
			"score":    0.5 + float64(r)/100,
			"active":   r%2 == 0,
			"retries":  int64(r / 3),
			"tags":     []any{"a", "b", "c"},
			"checksum": []byte{byte(r), byte(r >> 8), 0x00, 0xff},
			"verified": r%3 != 1,
		}
	}
	rows := []map[string]any{}
	for i := 1; i <= 64; i++ {
		rows = append(rows, scoped(i))
	}

	dictKeys := []string{"id", "ref", "name", "role", "score", "active", "retries", "tags", "checksum", "verified", "ingest", "transform", "load", "metadata", "created", "priority", "region", "host", "widget", "default"}
	dict := vela.NewDictionary(dictKeys...)

	deep := any(nil)
	for i := 0; i < 40; i++ { // 40 nested maps stays well under MaxDepth=64
		deep = map[string]any{"child": deep}
		deep.(map[string]any)["depth"] = int64(i)
	}
	wide := map[string]any{}
	for i := 0; i < 48; i++ { // 48 keys: forces the 31+ escape for map counts
		wide[fmt.Sprintf("field%02d", i)] = uint64(i)
	}
	longArray := []any{}
	for i := 0; i < 35; i++ { // 35 elements: forces the 31+ escape for array counts
		longArray = append(longArray, int64(-i))
	}
	longString := "lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor"
	outer := map[string]any{
		"meta": map[string]any{
			"kind":    "complex-sample",
			"version": uint64(1),
			"labels":  []any{"x", nil, false, 2.5},
		},
		"deep":      deep,
		"wide":      wide,
		"longs":     longArray,
		"text":      longString,
		"blob":      []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01, 0x02, 0x03},
		"unicode":   map[string]any{"emoji": "🚀", "greek": "Ωμέγα", "cjk": "日本語です"},
		"matrix":    []any{[]any{uint64(1), uint64(2), uint64(3)}, []any{int64(-1), int64(-2)}, []any{}},
		"empty_map": map[string]any{},
		"empty_arr": []any{},
	}

	return []sample{
		{"01_scalar_string.vlf", "hello vela", nil},
		{"02_person.vlf", person, nil},
		{"03_typed_scalars.vlf", map[string]any{
			"u8_small":  uint8(7),
			"u16":       uint16(300),
			"u32":       uint32(70000),
			"u64_large": uint64(^uint64(0) >> 1), // escapes to full uvarint
			"i8_neg":    int8(-1),
			"i32_neg":   int32(-300),
			"i64_zero":  int64(0),
			"int_max":   int64(1) << 62,
			"f32":       float32(3.5),
			"f64":       2.718281828459045,
			"t":         true,
			"f":         false,
			"nothing":   nil,
			"note":      "",
			"empty":     []any{},
			"no_keys":   map[string]any{},
			"digest":    []byte{0x01, 0x02, 0x03, 0x04},
		}, nil},
		{"04_nested.vlf", map[string]any{
			"user": map[string]any{
				"name":  "Sundar",
				"email": "sundar@example.com",
				"prefs": map[string]any{
					"theme":         "dark",
					"notifications": []any{map[string]any{"channel": "email", "on": true}, map[string]any{"channel": "sms", "on": false}},
					"locale":        "te-IN",
				},
			},
			"orders": []any{
				map[string]any{"sku": "WF-1", "qty": uint64(2), "price": 19.99, "paid": true},
				map[string]any{"sku": "WF-2", "qty": uint64(1), "price": 5.25, "paid": false},
			},
			"totals": []any{uint64(45), float64(25.24)},
			"note":   " Desarrollo ✔ 🚀 ",
		}, nil},
		{"05_repeated_rows.vlf", map[string]any{"table": "workers", "rows": rows}, nil},
		{"06_dictionary_rows.vlf", map[string]any{"table": "workers", "rows": rows}, dict},
		{"07_deep_complex.vlf", outer, nil},
	}
}

func main() {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}
	for _, s := range samples() {
		path := filepath.Join(outDir, s.name)
		var err error
		if s.dict != nil {
			err = vela.WriteFile(path, s.value, vela.FileOptions{Dictionary: s.dict})
		} else {
			err = vela.MarshalFile(path, s.value)
		}
		if err != nil {
			panic(fmt.Sprintf("write %s: %v", s.name, err))
		}
		owned, err := vela.UnmarshalFile(path)
		if err != nil {
			panic(fmt.Sprintf("round-trip %s: %v", s.name, err))
		}
		_, err = vela.OpenFileView(path, vela.FileOptions{})
		if err != nil {
			panic(fmt.Sprintf("view %s: %v", s.name, err))
		}
		st, err := os.Stat(path)
		if err != nil {
			panic(err)
		}
		kind := "plain"
		if s.dict != nil {
			kind = "dict"
		}
		fmt.Printf("%-26s %6d bytes  %4s  unmarshal=%T view-open=ok\n", s.name, st.Size(), kind, owned)
	}
	fmt.Println("done:", outDir)
}
