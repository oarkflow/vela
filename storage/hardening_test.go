package storage

import (
	"errors"
	"github.com/oarkflow/vela"
	"os"
	"path/filepath"
	"testing"
)

func TestExclusiveLockAndStreamingRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.vls")
	s, err := OpenWithLimits(path, Limits{MaxRecords: 2, MaxLiveBytes: 200})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second store opened: %v", err)
	}
	rec, _ := vela.Marshal(map[string]any{"name": "ash"})
	if err = s.Put("1", rec); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("2", rec); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("3", rec); !errors.Is(err, vela.ErrLimit) {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("after-close", rec); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte{0x80, 0x80})
	_ = f.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	defer s.Close()
	if s.Count() != 2 {
		t.Fatal(s.Count())
	}
	if err = s.Compact(); err != nil {
		t.Fatal(err)
	}
	if data, ok := s.Get("1"); !ok || len(data) != len(rec) {
		t.Fatalf("compacted record: %v", ok)
	}
}
