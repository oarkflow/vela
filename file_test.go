package vela

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileRoundTrip(t *testing.T) {
	value := map[string]any{"name": "Asha", "age": uint64(30), "alive": true, "parts": []any{int64(-5), []byte{0, 1, 255}, nil}}
	for _, dict := range []*Dictionary{nil, NewDictionary("name", "age", "alive", "parts")} {
		var b bytes.Buffer
		n, err := WriteTo(&b, value, FileOptions{Dictionary: dict})
		if err != nil || n != int64(b.Len()) {
			t.Fatalf("write: n=%d,err=%v", n, err)
		}
		decoded, err := ReadFrom(bytes.NewReader(b.Bytes()), FileOptions{})
		if err != nil || !reflect.DeepEqual(decoded, value) {
			t.Fatalf("roundtrip %v: %v vs %v (%v)", dict, decoded, value, err)
		}
		path := filepath.Join(t.TempDir(), "record.vela")
		if err := WriteFile(path, value, FileOptions{Dictionary: dict}); err != nil {
			t.Fatal(err)
		}
		view, err := OpenFileView(path, FileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		field, ok, err := view.Get("age")
		if err != nil || !ok {
			t.Fatal(err)
		}
		num, err := field.Uint()
		if err != nil || num != 30 {
			t.Fatal(num, err)
		}
		decoded, err = UnmarshalFile(path)
		if err != nil || !reflect.DeepEqual(decoded, value) {
			t.Fatalf("file roundtrip: %v, %v", decoded, err)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode %o", info.Mode())
		}
		existing, _ := os.ReadFile(path)
		cases := []struct {
			name string
			data []byte
		}{
			{"trunc", existing[:len(existing)-2]},
			{"corrupt", append([]byte(nil), existing...)},
			{"extra", append(append([]byte(nil), existing...), 42)},
			{"flags", append([]byte(nil), existing...)},
		}
		cases[1].data[len(cases[1].data)-7] ^= 0xff
		cases[3].data[4] = 0xff
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := os.WriteFile(path, tc.data, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := ReadFile(path, FileOptions{}); err == nil {
					t.Fatal("accepted invalid file")
				}
			})
		}
	}
}
func TestFileLimitsAndAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec.vela")
	orig := map[string]any{"k": "original"}
	if err := MarshalFile(path, orig); err != nil {
		t.Fatal(err)
	}
	err := WriteFile(path, map[string]any{"k": bytes.Repeat([]byte{1}, 2048)}, FileOptions{Limits: Limits{MaxBytes: 100}})
	if !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	got, err := UnmarshalFile(path)
	if err != nil || !reflect.DeepEqual(got, orig) {
		t.Fatal(got, err)
	}
	_, err = ReadFile(path, FileOptions{Limits: Limits{MaxBytes: 8}})
	if !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}
func TestDecodeLimits(t *testing.T) {
	tooMany, _ := Marshal([]any{uint64(1), uint64(2), uint64(3)})
	if _, err := UnmarshalWithLimits(tooMany, nil, Limits{MaxContainerEntries: 2}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := OpenWithLimits(tooMany, nil, Limits{MaxNodes: 2}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := OpenWithLimits(tooMany, nil, Limits{MaxBytes: 3}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := UnmarshalWithLimits(tooMany, nil, Limits{MaxDepth: 1}); err != nil {
		t.Fatal(err)
	}
	deep, _ := Marshal([]any{[]any{[]any{uint64(1)}}})
	if _, err := OpenWithLimits(deep, nil, Limits{MaxDepth: 2}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	longKey, _ := Marshal(map[string]any{"longkey": uint64(1)})
	if _, err := OpenWithLimits(longKey, nil, Limits{MaxKeyBytes: 2}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	noncanonical := []byte{0x1f, 0x01}
	if _, err := Unmarshal(noncanonical); err == nil {
		t.Fatal("noncanonical inline escape")
	}
}
func FuzzReadFileEnvelope(f *testing.F) {
	var buf bytes.Buffer
	_, _ = WriteTo(&buf, map[string]any{"a": uint64(7), "b": []byte{0, 1, 2}}, FileOptions{})
	f.Add(buf.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		_, _ = ReadFrom(bytes.NewReader(data), FileOptions{Limits: Limits{MaxBytes: 4096, MaxContainerEntries: 256, MaxNodes: 256, MaxKeyBytes: 256, MaxStringBytes: 2048}})
	})
}
