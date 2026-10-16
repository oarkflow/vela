package vela

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"
)

var benchRecord = map[string]any{"id": uint64(300), "age": uint64(30), "active": true, "name": "Asha"}
var benchJSON, _ = json.Marshal(benchRecord)
var benchVELA, _ = Marshal(benchRecord)
var benchDict = NewDictionary("id", "age", "active", "name")
var benchDictRecord, _ = MarshalWithDictionary(benchRecord, benchDict)
var benchSink []byte
var benchAny any
var benchView View
var benchNum uint64

type benchPerson struct {
	ID     uint64 `vela:"id" json:"id"`
	Age    uint64 `vela:"age" json:"age"`
	Active bool   `vela:"active" json:"active"`
	Name   string `vela:"name" json:"name"`
}

var benchStruct = benchPerson{300, 30, true, "Asha"}
var benchStructJSON, _ = json.Marshal(benchStruct)
var benchStructVELA, _ = Marshal(benchStruct)
var benchPersonSink benchPerson

func BenchmarkJSONMarshalStruct(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, err := json.Marshal(benchStruct)
		if err != nil {
			b.Fatal(err)
		}
		benchSink = v
	}
}

func BenchmarkVELAMarshalStruct(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, err := Marshal(benchStruct)
		if err != nil {
			b.Fatal(err)
		}
		benchSink = v
	}
}

func BenchmarkJSONUnmarshalStruct(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v benchPerson
		if err := json.Unmarshal(benchStructJSON, &v); err != nil {
			b.Fatal(err)
		}
		benchPersonSink = v
	}
}

func BenchmarkVELAUnmarshalStruct(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v benchPerson
		if err := UnmarshalInto(benchStructVELA, &v); err != nil {
			b.Fatal(err)
		}
		benchPersonSink = v
	}
}

func BenchmarkJSONMarshalMap(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchJSON)))
	for i := 0; i < b.N; i++ {
		v, e := json.Marshal(benchRecord)
		if e != nil {
			b.Fatal(e)
		}
		benchSink = v
	}
}
func BenchmarkVELAMarshalMap(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchVELA)))
	for i := 0; i < b.N; i++ {
		v, e := Marshal(benchRecord)
		if e != nil {
			b.Fatal(e)
		}
		benchSink = v
	}
}
func BenchmarkVELAMarshalDictionary(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchDictRecord)))
	for i := 0; i < b.N; i++ {
		v, e := MarshalWithDictionary(benchRecord, benchDict)
		if e != nil {
			b.Fatal(e)
		}
		benchSink = v
	}
}
func BenchmarkJSONUnmarshalMap(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchJSON)))
	for i := 0; i < b.N; i++ {
		var v any
		if e := json.Unmarshal(benchJSON, &v); e != nil {
			b.Fatal(e)
		}
		benchAny = v
	}
}
func BenchmarkVELAUnmarshalMap(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchVELA)))
	for i := 0; i < b.N; i++ {
		v, e := Unmarshal(benchVELA)
		if e != nil {
			b.Fatal(e)
		}
		benchAny = v
	}
}
func BenchmarkVELAOpenView(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchVELA)))
	for i := 0; i < b.N; i++ {
		v, e := Open(benchVELA)
		if e != nil {
			b.Fatal(e)
		}
		benchView = v
	}
}
func BenchmarkVELAGetID(b *testing.B) {
	v, _ := Open(benchVELA)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n, ok, e := v.GetUint("id")
		if e != nil || !ok {
			b.Fatal(e)
		}
		benchNum = n
	}
}
func BenchmarkVELAGetIDDictionary(b *testing.B) {
	v, _ := OpenWithDictionary(benchDictRecord, benchDict)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n, ok, e := v.GetUint("id")
		if e != nil || !ok {
			b.Fatal(e)
		}
		benchNum = n
	}
}
func BenchmarkJSONDecodeID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var v map[string]any
		if e := json.Unmarshal(benchJSON, &v); e != nil {
			b.Fatal(e)
		}
		benchNum = uint64(v["id"].(float64))
	}
}

// A preallocated, hand-written raw writer is not comparable to a generic JSON
// marshal: it is provided to measure the raw lower bound with no schema compiler.
func BenchmarkVELAWriterReuse(b *testing.B) {
	dst := make([]byte, 0, 64)
	w := NewWriter(dst)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Reset(dst[:0])
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
		result, e := w.Finish()
		if e != nil {
			b.Fatal(e)
		}
		benchSink = result
	}
}

// This comparator only encodes the equivalent fixed-schema message into the
// protobuf wire format. It is NOT the official protobuf library, and it is
// intentionally an unfair *schema-bound* size / speed lower bound.
func protoWire(dst []byte) []byte {
	dst = append(dst, 0x08)
	dst = binary.AppendUvarint(dst, 300)
	dst = append(dst, 0x10, 30, 0x18, 1, 0x22, 4)
	dst = append(dst, "Asha"...)
	return dst
}
func BenchmarkProtoWireFixedSchema(b *testing.B) {
	buf := make([]byte, 0, 32)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchSink = protoWire(buf[:0])
	}
}
func TestBenchSizes(t *testing.T) {
	proto := protoWire(nil)
	d, e := MarshalWithDictionary(benchRecord, benchDict)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("size bytes: JSON=%d VELA RAW=%d VELA DICT=%d fixed-schema protobuf wire=%d", len(benchJSON), len(benchVELA), len(d), len(proto))
	if len(proto) != 13 {
		t.Fatal(fmt.Sprintf("unexpected proto size %d", len(proto)))
	}
}
