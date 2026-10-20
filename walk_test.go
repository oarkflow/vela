package vela

import "testing"

func TestWalkBorrowed(t *testing.T) {
	dict := NewDictionary("known")
	data, err := MarshalWithDictionary(map[string]any{"known": []any{uint64(1), uint64(2)}, "inline": "ok"}, dict)
	if err != nil {
		t.Fatal(err)
	}
	view, err := OpenWithDictionary(data, dict)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	err = view.WalkMap(func(key Key, value View) bool {
		if key.Equal("known") {
			sum := uint64(0)
			if err := value.WalkArray(func(_ int, element View) bool {
				n, e := element.Uint()
				if e != nil {
					t.Fatal(e)
				}
				sum += n
				return true
			}); err != nil {
				t.Fatal(err)
			}
			if sum != 3 {
				t.Fatal(sum)
			}
			found++
		}
		if key.Equal("inline") {
			found++
		}
		return true
	})
	if err != nil || found != 2 {
		t.Fatal(err, found)
	}
	if err := view.WalkArray(func(int, View) bool { return true }); err != ErrType {
		t.Fatal(err)
	}
}

func BenchmarkWalkMapNoAllocation(b *testing.B) {
	v, err := Open(benchVELA)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		err := v.WalkMap(func(key Key, value View) bool {
			if key.Equal("id") {
				n, e := value.Uint()
				if e != nil {
					b.Fatal(e)
				}
				benchNum = n
			}
			return true
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
