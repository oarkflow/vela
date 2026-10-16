package vela

import (
	"bufio"
	"bytes"
	"testing"
)

func TestFrame(t *testing.T) {
	v, _ := Marshal(map[string]any{"id": uint64(5)})
	frame := AppendFrame(nil, v)
	p, e := ReadFrame(bufio.NewReader(bytes.NewReader(frame)), 100)
	if e != nil || !bytes.Equal(v, p) {
		t.Fatal(e, p)
	}
	if _, e = ReadFrame(bufio.NewReader(bytes.NewReader(frame)), 1); e == nil {
		t.Fatal("length cap ignored")
	}
}
func TestDictionaryConcurrent(t *testing.T) {
	d := NewDictionary("id", "name")
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			d.Lookup("id")
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		d.Intern("new")
	}
	<-done
}
func TestFrameMalformedHeaders(t *testing.T) {
	// Escape to an overlong 1-byte size is not canonical.
	invalid := [][]byte{{0x80, 0x00, 0x01}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02}}
	for _, v := range invalid {
		if _, err := ReadFrame(bufio.NewReader(bytes.NewReader(v)), 1<<20); err == nil {
			t.Fatal(v)
		}
	}
	var dst bytes.Buffer
	_, err := WriteFrame(&dst, []byte{0x40}, 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteFrame(&dst, []byte{0x40, 0x41}, 2)
	if err == nil {
		t.Fatal("oversized frame accepted")
	}
}
