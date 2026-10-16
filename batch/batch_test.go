package batch

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/oarkflow/vela"
)

func TestBatch(t *testing.T) {
	rows := make([]map[string]any, 300)
	for i := range rows {
		rows[i] = map[string]any{"id": uint64(10000 + i), "active": i%2 == 0, "status": []string{"paid", "new", "paid"}[i%3], "nullable": nil, "changing": int64(i%17 - 9)}
	}
	enc, e := Marshal(rows)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Open(enc)
	if e != nil {
		t.Fatal(e)
	}
	if b.Len() != 300 {
		t.Fatal(b.Len())
	}
	for i, row := range rows {
		for _, name := range b.Columns() {
			v, _, e := b.Get(i, name)
			if e != nil {
				t.Fatal(i, name, e)
			}
			if name == "changing" {
				if v.(int64) != row[name].(int64) {
					t.Fatal(v, row[name])
				}
			} else if !reflect.DeepEqual(v, row[name]) {
				t.Fatal(i, name, v, row[name])
			}
		}
	}
	ids, e := b.FilterEq("status", "new")
	if e != nil || len(ids) != 100 {
		t.Fatal(len(ids), e)
	}
	proj, e := b.Project(4, "id", "status")
	if e != nil || proj["id"] != uint64(10004) {
		t.Fatal(proj, e)
	}
	if len(enc) >= 300*90 {
		t.Fatal("unreasonably large batch")
	}
}
func TestMalformed(t *testing.T) {
	for _, v := range [][]byte{nil, []byte("VLB1"), []byte("VLB1\x01\x01\x02xx\xff\x00")} {
		if _, e := Open(v); e == nil {
			t.Fatal(v)
		}
	}
}
func FuzzOpen(f *testing.F) {
	b, _ := Marshal([]map[string]any{{"k": "v"}})
	f.Add(b)
	f.Fuzz(func(t *testing.T, data []byte) {
		o, e := Open(data)
		if e == nil && o.Len() > 0 {
			for _, c := range o.Columns() {
				_, _, _ = o.Get(0, c)
			}
		}
	})
}
func BenchmarkBatchEncode(b *testing.B) {
	rows := make([]map[string]any, 1024)
	for i := range rows {
		rows[i] = map[string]any{"id": uint64(100000 + i), "active": i%2 == 0, "status": []string{"paid", "new", "closed"}[i%3]}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, e := Marshal(rows)
		if e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkBatchFilter(b *testing.B) {
	rows := make([]map[string]any, 1024)
	for i := range rows {
		rows[i] = map[string]any{"id": uint64(100000 + i), "active": i%2 == 0, "status": []string{"paid", "new", "closed"}[i%3]}
	}
	enc, _ := Marshal(rows)
	v, _ := Open(enc)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, e := v.FilterEq("status", "paid")
		if e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkRepeatedRows(b *testing.B) {
	rows := make([]map[string]any, 1024)
	for i := range rows {
		rows[i] = map[string]any{"id": uint64(100000 + i), "active": i%2 == 0, "status": []string{"paid", "new", "closed"}[i%3]}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range rows {
			_, e := vela.Marshal(r)
			if e != nil {
				b.Fatal(e)
			}
		}
	}
}

func BenchmarkBatchEncodeFast(b *testing.B) {
	rows := make([]map[string]any, 1024)
	for i := range rows {
		rows[i] = map[string]any{"id": uint64(100000 + i), "active": i%2 == 0, "status": []string{"paid", "new", "closed"}[i%3]}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, e := MarshalFast(rows)
		if e != nil {
			b.Fatal(e)
		}
	}
}
func TestFastBatch(t *testing.T) {
	rows := make([]map[string]any, 100)
	for i := range rows {
		rows[i] = map[string]any{"id": uint64(i), "status": "paid", "bool": i%2 == 0, "mixed": int64(-i)}
	}
	data, e := MarshalFast(rows)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Open(data)
	if e != nil {
		t.Fatal(e)
	}
	for i := range rows {
		for k, v := range rows[i] {
			got, ok, e := b.Get(i, k)
			if e != nil || !ok || !reflect.DeepEqual(got, v) {
				t.Fatal(i, k, got, v, e)
			}
		}
	}
}

func standardBatch() []map[string]any {
	rows := make([]map[string]any, 1024)
	for i := range rows {
		rows[i] = map[string]any{"id": uint64(100000 + i), "active": i%2 == 0, "status": []string{"paid", "new", "closed"}[i%3]}
	}
	return rows
}
func TestBatchSizes(t *testing.T) {
	rows := standardBatch()
	jb, _ := json.Marshal(rows)
	vb, _ := Marshal(rows)
	fast, _ := MarshalFast(rows)
	var separate int
	for _, r := range rows {
		b, _ := vela.Marshal(r)
		separate += len(b)
	}
	t.Logf("1024 rows: JSON=%d B; VELA raw rows=%d B; VELA TILE=%d B; VELA TILE-Fast=%d B", len(jb), separate, len(vb), len(fast))
}
func BenchmarkJSONBatchEncode(b *testing.B) {
	rows := standardBatch()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, e := json.Marshal(rows)
		if e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkJSONBatchFilter(b *testing.B) {
	rows := standardBatch()
	data, _ := json.Marshal(rows)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded []map[string]any
		if e := json.Unmarshal(data, &decoded); e != nil {
			b.Fatal(e)
		}
		hits := 0
		for _, r := range decoded {
			if r["status"] == "paid" {
				hits++
			}
		}
		if hits == 0 {
			b.Fatal("no results")
		}
	}
}

func BenchmarkVELABatchOpenAndFilter(b *testing.B) {
	rows := standardBatch()
	data, _ := MarshalFast(rows)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, e := Open(data)
		if e != nil {
			b.Fatal(e)
		}
		matches, e := page.FilterEq("status", "paid")
		if e != nil || len(matches) == 0 {
			b.Fatal(e)
		}
	}
}
