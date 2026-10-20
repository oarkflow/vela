package vela

import (
	"bytes"
	"unicode/utf8"
)

// Key is a borrowed map key. String converts an inline key to an owned string;
// Equal compares without allocating. Keep the View's buffer alive while using it.
type Key struct {
	raw  []byte
	text string
}

func (k Key) Equal(s string) bool {
	if k.raw != nil {
		return bytes.Equal(k.raw, []byte(s))
	}
	return k.text == s
}

func (k Key) String() string {
	if k.raw != nil {
		return string(k.raw)
	}
	return k.text
}

func (r *Reader) borrowedKey() (Key, error) {
	t, err := r.readVar()
	if err != nil {
		return Key{}, err
	}
	if t&1 == 0 {
		if t == 0 || r.dict == nil || t>>1 > 1<<20 {
			return Key{}, ErrDictionary
		}
		s, ok := r.dict.Get(uint32(t >> 1))
		if !ok {
			return Key{}, ErrDictionary
		}
		return Key{text: s}, nil
	}
	if t>>1 > uint64(r.limits.MaxKeyBytes) {
		return Key{}, ErrLimit
	}
	b, err := r.readBytes(t >> 1)
	if err != nil {
		return Key{}, err
	}
	// A View was fully validated by Open, but this also keeps borrowedKey safe
	// if called by a future reader path.
	if !utf8.Valid(b) {
		return Key{}, ErrInvalid
	}
	return Key{raw: b}, nil
}

// WalkMap visits entries in wire order without materializing a map. Returning
// false stops the walk. Keys and values borrow the View's underlying bytes.
func (v View) WalkMap(visit func(Key, View) bool) error {
	r := Reader{data: v.data, dict: v.dict, limits: v.limits.normalized()}
	kind, count, err := r.readHead()
	if err != nil {
		return err
	}
	if kind != KindMap {
		return ErrType
	}
	for i := uint64(0); i < count; i++ {
		key, err := r.borrowedKey()
		if err != nil {
			return err
		}
		start := r.pos
		if err := r.skip(1); err != nil {
			return err
		}
		if !visit(key, View{data: r.data[start:r.pos], dict: v.dict, limits: v.limits}) {
			return nil
		}
	}
	return nil
}

// WalkArray visits elements in wire order without materializing a slice.
func (v View) WalkArray(visit func(int, View) bool) error {
	r := Reader{data: v.data, dict: v.dict, limits: v.limits.normalized()}
	kind, count, err := r.readHead()
	if err != nil {
		return err
	}
	if kind != KindArray {
		return ErrType
	}
	for i := uint64(0); i < count; i++ {
		start := r.pos
		if err := r.skip(1); err != nil {
			return err
		}
		if !visit(int(i), View{data: r.data[start:r.pos], dict: v.dict, limits: v.limits}) {
			return nil
		}
	}
	return nil
}
