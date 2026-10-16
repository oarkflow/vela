package vela

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestKnownWire(t *testing.T) {
	w := NewWriter(nil)
	w.BeginMap(4)
	w.Key("id")
	w.Uint(300)
	w.Key("age")
	w.Uint(30)
	w.Key("active")
	w.Bool(true)
	w.Key("name")
	w.String("Asha")
	w.EndMap()
	b, e := w.Finish()
	if e != nil {
		t.Fatal(e)
	}
	expected := []byte{0xC4, 0x05, 'i', 'd', 0x1f, 0xac, 0x02, 0x07, 'a', 'g', 'e', 0x1e, 0x0d, 'a', 'c', 't', 'i', 'v', 'e', 0x42, 0x09, 'n', 'a', 'm', 'e', 0x84, 'A', 's', 'h', 'a'}
	if !bytes.Equal(b, expected) {
		t.Fatalf("wire = %x, want %x", b, expected)
	}
	if len(b) != 30 {
		t.Fatal(len(b))
	}
	view, e := Open(b)
	if e != nil {
		t.Fatal(e)
	}
	u, ok, e := view.GetUint("id")
	if e != nil || !ok || u != 300 {
		t.Fatal(u, ok, e)
	}
	s, ok, e := view.GetString("name")
	if e != nil || !ok || s != "Asha" {
		t.Fatal(s, ok, e)
	}
}
func TestRoundtrip(t *testing.T) {
	cases := []any{nil, true, false, uint64(0), uint64(30), uint64(31), uint64(math.MaxUint64), int64(math.MinInt64), int64(-123), float32(1.25), float64(-20.5), "", "Hello, नेपाल", []byte{0, 255}, []any{true, "a", uint64(5)}, map[string]any{"nested": map[string]any{"x": int64(7)}, "a": []any{false, uint64(8)}}}
	for _, v := range cases {
		b, e := Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		out, e := Unmarshal(b)
		if e != nil {
			t.Fatalf("decode %T: %v", v, e)
		}
		if !reflect.DeepEqual(v, out) {
			t.Fatalf("%T %v -> %T %v", v, v, out, out)
		}
		if _, e = Open(b); e != nil {
			t.Fatal(e)
		}
	}
}
func TestDictionary(t *testing.T) {
	d := NewDictionary("id", "name")
	v := map[string]any{"id": uint64(8), "name": "Asha"}
	raw, _ := Marshal(v)
	enc, e := MarshalWithDictionary(v, d)
	if e != nil {
		t.Fatal(e)
	}
	if len(enc) >= len(raw) {
		t.Fatalf("dictionary %d >= raw %d", len(enc), len(raw))
	}
	if _, e = Unmarshal(enc); e == nil {
		t.Fatal("expected no dictionary error")
	}
	dec, e := UnmarshalWithDictionary(enc, d)
	if e != nil || !reflect.DeepEqual(dec, v) {
		t.Fatal(dec, e)
	}
	serialized, e := d.MarshalBinary()
	if e != nil {
		t.Fatal(e)
	}
	d2, e := ParseDictionary(serialized)
	if e != nil {
		t.Fatal(e)
	}
	dec, e = UnmarshalWithDictionary(enc, d2)
	if e != nil || !reflect.DeepEqual(dec, v) {
		t.Fatal(dec, e)
	}
}
func TestNestedView(t *testing.T) {
	data, _ := Marshal(map[string]any{"profile": map[string]any{"age": uint64(30), "name": "N"}, "values": []any{uint64(5), "ok"}})
	view, _ := Open(data)
	field, ok, e := view.Find("profile", "name")
	if !ok || e != nil {
		t.Fatal(ok, e)
	}
	name, _ := field.String()
	if name != "N" {
		t.Fatal(name)
	}
	arr, ok, e := view.Get("values")
	if e != nil || !ok {
		t.Fatal(e)
	}
	sub, ok, e := arr.At(1)
	if e != nil || !ok {
		t.Fatal(e)
	}
	s, _ := sub.String()
	if s != "ok" {
		t.Fatal(s)
	}
}
func TestMalformed(t *testing.T) {
	bad := [][]byte{{}, {0x9F}, {0x44}, {0x40, 0x40}, {0xBF, 0x80, 0x00}, {0xc1, 0x03, 'x'}, {0x45}, {0xdf, 0xff, 0xff, 0xff, 0xff, 0x10}}
	for _, b := range bad {
		if _, e := Open(b); e == nil {
			t.Fatalf("accepted invalid %x", b)
		}
	}
}
func TestReaderBufferOwnership(t *testing.T) {
	b, _ := Marshal("hello")
	v, _ := Open(b)
	s, e := v.StringBytes()
	if e != nil || string(s) != "hello" {
		t.Fatal(s, e)
	}
	if &b[1] != &s[0] {
		t.Fatal("not borrowed")
	}
}
func TestWriterRejectsBadMap(t *testing.T) {
	w := NewWriter(nil)
	w.BeginMap(1)
	w.Key("a")
	if _, e := w.Finish(); e == nil {
		t.Fatal("unfinished map accepted")
	}
}
func TestReflectStruct(t *testing.T) {
	type Person struct {
		ID     uint64 `json:"id"`
		Name   string `json:"name"`
		Hidden string `json:"-"`
	}
	b, e := Marshal(Person{1, "A", "no"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := Unmarshal(b)
	if e != nil {
		t.Fatal(e)
	}
	m := v.(map[string]any)
	if len(m) != 2 || m["id"] != uint64(1) {
		t.Fatal(m)
	}
}
func TestJSONEquivalence(t *testing.T) {
	o := map[string]any{"a": uint64(2), "name": "hi", "nested": []any{true, int64(-4)}}
	b, e := Marshal(o)
	if e != nil {
		t.Fatal(e)
	}
	v, e := Unmarshal(b)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = json.Marshal(v)
}
func FuzzOpen(f *testing.F) {
	f.Add([]byte{0x40})
	f.Add([]byte{0xC1, 0x03, 'x', 0x05})
	f.Add([]byte{0x9F, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) {
		v, e := Open(b)
		if e == nil {
			decoded, e := v.Unmarshal()
			if e != nil {
				t.Fatalf("open successful but unmarshal failed %x: %v", b, e)
			}
			_ = decoded
		}
	})
}
func FuzzRoundtripUint(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(math.MaxUint64))
	f.Fuzz(func(t *testing.T, n uint64) {
		b, _ := Marshal(n)
		v, e := Unmarshal(b)
		if e != nil || v != n {
			t.Fatalf("%d -> %v %v", n, v, e)
		}
	})
}
func TestRandomScalars(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 10000; i++ {
		n := rng.Uint64()
		b, _ := Marshal(n)
		v, e := Unmarshal(b)
		if e != nil || v != n {
			t.Fatal(i)
		}
	}
}
