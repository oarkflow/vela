package vela

import (
	"errors"
	"strings"
	"testing"
)

func TestEncodeLimited(t *testing.T) {
	_, err := MarshalLimited(map[string]any{"huge": strings.Repeat("x", 2000)}, nil, Limits{MaxBytes: 100})
	if !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	val := map[string]any{"id": uint64(123), "name": "Asha"}
	b, err := MarshalLimited(val, nil, Limits{MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Unmarshal(b); err != nil {
		t.Fatal(err)
	}
}
