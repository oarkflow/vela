package batch

import (
	"path/filepath"
	"testing"
)

func TestBatchDiskRoundTrip(t *testing.T) {
	rows := []map[string]any{{"id": uint64(9), "active": true, "name": "A"}, {"id": uint64(10), "active": false, "name": "B"}}
	path := filepath.Join(t.TempDir(), "batch.vlf")
	if err := WriteFile(path, rows); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Len() != 2 {
		t.Fatal(decoded.Len())
	}
	v, ok, err := decoded.Get(1, "name")
	if err != nil || !ok || v != "B" {
		t.Fatal(v, ok, err)
	}
}
