package batch

import (
	"errors"
	"testing"

	"github.com/oarkflow/vela"
)

func TestBitpackAcrossBoundaries(t *testing.T) {
	for width := uint8(0); width <= 64; width++ {
		raw := make([]byte, (uint64(37)*uint64(width)+7)/8)
		for i := 0; i < 37; i++ {
			var n uint64
			if width == 64 {
				n = uint64(i) * 0x123456789abcdef
			}
			if width > 0 && width < 64 {
				n = uint64(i) & ((uint64(1) << width) - 1)
			}
			packedPut(raw, i, width, n)
		}
		for i := 0; i < 37; i++ {
			var want uint64
			if width == 64 {
				want = uint64(i) * 0x123456789abcdef
			}
			if width > 0 && width < 64 {
				want = uint64(i) & ((uint64(1) << width) - 1)
			}
			if got := packedGet(raw, i, width); got != want {
				t.Fatalf("width=%d index=%d got=%d want=%d", width, i, got, want)
			}
		}
	}
}
func TestBatchLimits(t *testing.T) {
	rows := []map[string]any{{"age": uint64(1)}, {"age": uint64(2)}}
	raw, err := MarshalFast(rows)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenWithLimits(raw, Limits{MaxRows: 1}); !errors.Is(err, ErrBatch) {
		t.Fatal(err)
	}
	if _, err := OpenWithLimits(raw, Limits{MaxBytes: 5}); !errors.Is(err, vela.ErrLimit) {
		t.Fatal(err)
	}
	if _, err := OpenWithLimits(raw, Limits{MaxColumns: 0}); err != nil {
		t.Fatal(err)
	}
}
