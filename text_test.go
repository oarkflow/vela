package vela

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestTextTypedRoundTrip(t *testing.T) {
	type message struct {
		ID   uint64 `vela:"id"`
		Text string `vela:"text"`
	}
	in := message{ID: 42, Text: "hello"}
	encoded, err := MarshalText(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{id=u42;text="hello"}` {
		t.Fatalf("text = %s", encoded)
	}
	var out message
	if err := UnmarshalTextInto(encoded, &out, Limits{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %#v", out)
	}
}

func TestTextAllKinds(t *testing.T) {
	data := []byte(`{items=[u1;i-2;f32(1.5);fNaN;true;false;null;b64"AAE=";{x="hi"}]}`)
	v, err := UnmarshalText(data)
	if err != nil {
		t.Fatal(err)
	}
	items := v.(map[string]any)["items"].([]any)
	if len(items) != 9 || items[0] != uint64(1) || items[1] != int64(-2) || items[2] != float32(1.5) || !math.IsNaN(items[3].(float64)) || items[4] != true || items[5] != false || items[6] != nil || !bytes.Equal(items[7].([]byte), []byte{0, 1}) || items[8].(map[string]any)["x"] != "hi" {
		t.Fatalf("items = %#v", items)
	}
	encoded, err := MarshalText(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip = %s", encoded)
	}
}

func TestTextDatetimeDuration(t *testing.T) {
	type record struct {
		When    time.Time     `vela:"when"`
		Pointer *time.Time    `vela:"pointer"`
		Elapsed time.Duration `vela:"elapsed"`
	}
	when := time.Date(2026, 10, 16, 1, 2, 3, 0, time.UTC)
	in := record{When: when, Pointer: &when, Elapsed: 90 * time.Minute}
	data, err := MarshalText(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{elapsed=d"1h30m0s";pointer=t"2026-10-16T01:02:03Z";when=t"2026-10-16T01:02:03Z"}` {
		t.Fatal(got)
	}
	var out record
	if err := UnmarshalTextInto(data, &out, Limits{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %#v", out)
	}
}

func TestTextRejectsMalformedAndLimited(t *testing.T) {
	for _, data := range []string{
		`{"id":42}`, `map{id=42}`, `map{id=u1;id=u2}`, `map{id=u18446744073709551616}`,
		`map{id=i-9223372036854775809}`, `map{id=b64"!"}`, `map{id=s"x"} garbage`,
		`[u1;]`, `map{id=u1;}`, `f32(nope)`, `f32(0x1p2)`,
	} {
		if _, err := UnmarshalText([]byte(data)); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	if _, err := UnmarshalTextWithLimits([]byte(`map{id=u42}`), Limits{MaxBytes: 5}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := UnmarshalTextWithLimits([]byte(`[[u1]]`), Limits{MaxDepth: 1}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}

func TestTextStringAndFloatPrefixes(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  any
	}{
		{`"hello"`, "hello"},
		{`s"hello"`, "hello"},
		{`f1.5`, float64(1.5)},
		{`f32(1.5)`, float32(1.5)},
		{`true`, true},
		{`false`, false},
	} {
		got, err := UnmarshalText([]byte(tc.input))
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %v, %v", tc.input, got, err)
		}
	}
}
