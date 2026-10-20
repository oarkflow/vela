package vela

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTypedRoundTrip(t *testing.T) {
	type alias string
	type child struct {
		Name string `json:"name"`
		Drop string `vela:"-"`
	}
	type record struct {
		ID       uint32            `vela:"id" json:"wrong"`
		Children []child           `vela:"children"`
		Scores   map[alias][]int16 `json:"scores"`
		Payload  [3]byte           `vela:"payload"`
		When     time.Time         `vela:"when"`
		Optional *string           `vela:"optional,omitempty"`
		Missing  string            `vela:"missing,omitempty"`
	}
	in := record{ID: 31, Children: []child{{Name: "A", Drop: "secret"}}, Scores: map[alias][]int16{"a": {-3, 5}}, Payload: [3]byte{0, 1, 255}, When: time.Date(2026, 10, 16, 1, 2, 3, 0, time.UTC)}
	b, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out record
	if err := UnmarshalInto(b, &out); err != nil {
		t.Fatal(err)
	}
	in.Children[0].Drop = ""
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %#v, want %#v", out, in)
	}
	m, err := Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(map[string]any)["missing"]; ok {
		t.Fatal("omitempty field was encoded")
	}
	if _, ok := m.(map[string]any)["wrong"]; ok {
		t.Fatal("json tag superseded vela tag")
	}
}

func TestTypedNilAndEmpty(t *testing.T) {
	for _, value := range []any{[]any(nil), []byte(nil), map[string]any(nil), []string(nil), map[string]int(nil)} {
		b, err := Marshal(value)
		if err != nil || !reflect.DeepEqual(b, []byte{Null}) {
			t.Fatalf("%T: %x, %v", value, b, err)
		}
	}
	var nilSlice []int
	var emptySlice = []int{}
	for _, tc := range []struct {
		input   any
		wantNil bool
	}{{nilSlice, true}, {emptySlice, false}} {
		b, err := Marshal(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		if err := UnmarshalInto(b, &out); err != nil {
			t.Fatal(err)
		}
		if (out == nil) != tc.wantNil {
			t.Fatalf("nil=%v, want %v", out == nil, tc.wantNil)
		}
	}
	b, err := Marshal([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	var bytesOut []byte
	if err := UnmarshalInto(b, &bytesOut); err != nil || bytesOut == nil || len(bytesOut) != 0 {
		t.Fatalf("typed empty bytes: %v, %v", bytesOut, err)
	}
	generic, err := Unmarshal(b)
	if err != nil || generic.([]byte) == nil {
		t.Fatalf("generic empty bytes: %v, %v", generic, err)
	}
}

func TestTypedDecodeErrors(t *testing.T) {
	for _, tc := range []struct {
		value  any
		target any
	}{
		{uint64(256), new(uint8)},
		{int64(-129), new(int8)},
		{uint64(1), new(int)},
		{[]any{uint64(1)}, new([2]uint64)},
		{map[string]any{"x": "bad"}, new(map[string]uint64)},
	} {
		b, err := Marshal(tc.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := UnmarshalInto(b, tc.target); !errors.Is(err, ErrType) {
			t.Fatalf("%T -> %T: %v", tc.value, tc.target, err)
		}
	}
	b, _ := Marshal(uint64(1))
	if err := UnmarshalInto(b, nil); !errors.Is(err, ErrType) {
		t.Fatal(err)
	}
	var dst uint8 = 9
	b, _ = Marshal(uint64(256))
	if err := UnmarshalInto(b, &dst); !errors.Is(err, ErrType) || dst != 9 {
		t.Fatal(err, dst)
	}
}

func TestTypedVLFFileRoundTrip(t *testing.T) {
	type item struct {
		ID         uint64           `vela:"id"`
		Labels     []string         `vela:"labels"`
		Attributes map[string]int64 `vela:"attributes"`
	}
	in := item{ID: 300, Labels: []string{"a", "b"}, Attributes: map[string]int64{"score": -3}}
	path := filepath.Join(t.TempDir(), "record.vlf")
	dict := NewDictionary("id", "labels", "attributes", "score")
	if err := WriteFile(path, in, FileOptions{Dictionary: dict}); err != nil {
		t.Fatal(err)
	}
	var out item
	if err := UnmarshalFileInto(path, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %#v, want %#v", out, in)
	}
}

func TestTypedDuplicateStructFields(t *testing.T) {
	// A repeated wire key keeps the first value, including for structs with
	// enough fields to use the decoder's larger duplicate tracker.
	for _, count := range []int{1, 65} {
		fields := make([]reflect.StructField, count)
		for i := range fields {
			fields[i] = reflect.StructField{
				Name: fmt.Sprintf("Field%d", i),
				Type: reflect.TypeOf(uint64(0)),
				Tag:  reflect.StructTag(fmt.Sprintf(`vela:"field%d"`, i)),
			}
		}
		target := reflect.New(reflect.StructOf(fields))
		w := NewWriter(nil)
		w.BeginMap(2)
		key := fmt.Sprintf("field%d", count-1)
		w.Key(key)
		w.Uint(7)
		w.Key(key)
		w.Uint(9)
		w.EndMap()
		data, err := w.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if err := UnmarshalInto(data, target.Interface()); err != nil {
			t.Fatal(err)
		}
		if got := target.Elem().Field(count - 1).Uint(); got != 7 {
			t.Fatalf("%d fields: got %d, want 7", count, got)
		}
	}
}
