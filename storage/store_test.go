package storage

import (
	"github.com/oarkflow/vela"
	"path/filepath"
	"testing"
)

func TestStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.vela")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := vela.Marshal(map[string]any{"id": uint64(1), "status": "paid"})
	b, _ := vela.Marshal(map[string]any{"id": uint64(2), "status": "new"})
	if e = s.Put("a", a); e != nil {
		t.Fatal(e)
	}
	if e = s.Put("b", b); e != nil {
		t.Fatal(e)
	}
	if e = s.Delete("a"); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.Count() != 1 {
		t.Fatal(s.Count())
	}
	got, ok := s.Get("b")
	if !ok || string(got) != string(b) {
		t.Fatal(ok, got)
	}
	matches, e := s.FilterEq("status", "new")
	if e != nil || len(matches) != 1 {
		t.Fatal(matches, e)
	}
	if e = s.Compact(); e != nil {
		t.Fatal(e)
	}
	if s.Count() != 1 {
		t.Fatal(s.Count())
	}
}
