package vela

import (
	"encoding/binary"
	"math"
	"unicode/utf8"
)

// Writer writes raw values without maps, reflection, or temporary objects.
// The caller must balance BeginMap / EndMap and BeginArray / EndArray, and
// supply precisely the announced number of entries. Finish validates counts.
type Writer struct {
	buf    []byte
	frames []writerFrame
	err    error
	roots  int
}
type writerFrame struct {
	kind      byte
	remaining int
	expectKey bool
}

func NewWriter(dst []byte) *Writer { return &Writer{buf: dst} }
func (w *Writer) Reset(dst []byte) { w.buf = dst; w.frames = w.frames[:0]; w.err = nil; w.roots = 0 }
func (w *Writer) beforeValue() {
	if w.err != nil {
		return
	}
	if len(w.frames) == 0 {
		w.roots++
		if w.roots > 1 {
			w.err = ErrInvalid
		}
		return
	}
	f := &w.frames[len(w.frames)-1]
	if f.remaining <= 0 || (f.kind == KindMap && f.expectKey) {
		w.err = ErrInvalid
		return
	}
	f.remaining--
	if f.kind == KindMap {
		f.expectKey = true
	}
}
func (w *Writer) Key(s string) {
	if w.err != nil {
		return
	}
	if len(w.frames) == 0 {
		w.err = ErrInvalid
		return
	}
	f := &w.frames[len(w.frames)-1]
	if f.kind != KindMap || !f.expectKey || f.remaining <= 0 {
		w.err = ErrInvalid
		return
	}
	if !utf8.ValidString(s) {
		w.err = ErrInvalid
		return
	}
	w.buf = appendKey(w.buf, s, nil)
	f.expectKey = false
}
func (w *Writer) Uint(x uint64) {
	w.beforeValue()
	if w.err == nil {
		w.buf = head(w.buf, KindUint, x)
	}
}
func (w *Writer) Int(x int64) {
	w.beforeValue()
	if w.err == nil {
		w.buf = head(w.buf, KindInt, zz(x))
	}
}
func (w *Writer) String(s string) {
	if !utf8.ValidString(s) {
		w.err = ErrInvalid
		return
	}
	w.beforeValue()
	if w.err == nil {
		w.buf = head(w.buf, KindString, uint64(len(s)))
		w.buf = append(w.buf, s...)
	}
}
func (w *Writer) Bytes(x []byte) {
	w.beforeValue()
	if w.err == nil {
		w.buf = head(w.buf, KindBytes, uint64(len(x)))
		w.buf = append(w.buf, x...)
	}
}
func (w *Writer) Bool(x bool) {
	w.beforeValue()
	if w.err == nil {
		if x {
			w.buf = append(w.buf, True)
		} else {
			w.buf = append(w.buf, False)
		}
	}
}
func (w *Writer) Null() {
	w.beforeValue()
	if w.err == nil {
		w.buf = append(w.buf, Null)
	}
}
func (w *Writer) Float64(x float64) {
	w.beforeValue()
	if w.err == nil {
		w.buf = binary.LittleEndian.AppendUint64(append(w.buf, Float64), math.Float64bits(x))
	}
}
func (w *Writer) BeginMap(n int) {
	w.beforeValue()
	if w.err != nil {
		return
	}
	if n < 0 || len(w.frames) >= MaxDepth {
		w.err = ErrLimit
		return
	}
	w.buf = head(w.buf, KindMap, uint64(n))
	w.frames = append(w.frames, writerFrame{kind: KindMap, remaining: n, expectKey: true})
}
func (w *Writer) BeginArray(n int) {
	w.beforeValue()
	if w.err != nil {
		return
	}
	if n < 0 || len(w.frames) >= MaxDepth {
		w.err = ErrLimit
		return
	}
	w.buf = head(w.buf, KindArray, uint64(n))
	w.frames = append(w.frames, writerFrame{kind: KindArray, remaining: n})
}
func (w *Writer) end(kind byte) {
	if w.err != nil {
		return
	}
	if len(w.frames) == 0 {
		w.err = ErrInvalid
		return
	}
	f := w.frames[len(w.frames)-1]
	if f.kind != kind || f.remaining != 0 || (kind == KindMap && !f.expectKey) {
		w.err = ErrInvalid
		return
	}
	w.frames = w.frames[:len(w.frames)-1]
}
func (w *Writer) EndMap()   { w.end(KindMap) }
func (w *Writer) EndArray() { w.end(KindArray) }
func (w *Writer) Finish() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	if w.roots != 1 || len(w.frames) != 0 {
		return nil, ErrInvalid
	}
	return w.buf, nil
}
