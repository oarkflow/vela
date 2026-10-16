package vela

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// Dictionary uses immutable snapshots for lock-free lookups. Updates copy the
// current small dictionary, then publish atomically. Register keys primarily
// at connection/file setup, not in a hot encoding loop.
type Dictionary struct {
	mu       sync.Mutex
	snapshot atomic.Pointer[dictSnapshot]
}
type dictSnapshot struct {
	keys []string
	ids  map[string]uint32
}

func NewDictionary(keys ...string) *Dictionary {
	d := &Dictionary{}
	snap := &dictSnapshot{ids: make(map[string]uint32, len(keys))}
	for _, key := range keys {
		if !utf8.ValidString(key) {
			continue
		}
		if snap.ids[key] != 0 {
			continue
		}
		if len(snap.keys) >= 1<<20 {
			break
		}
		id := uint32(len(snap.keys) + 1)
		snap.keys = append(snap.keys, key)
		snap.ids[key] = id
	}
	d.snapshot.Store(snap)
	return d
}
func (d *Dictionary) read() *dictSnapshot {
	if d == nil {
		return nil
	}
	return d.snapshot.Load()
}
func (d *Dictionary) Intern(k string) uint32 {
	if !utf8.ValidString(k) {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.read()
	if old == nil {
		old = &dictSnapshot{ids: make(map[string]uint32)}
	}
	if id := old.ids[k]; id != 0 {
		return id
	}
	if len(old.keys) >= 1<<20 {
		return 0
	}
	next := &dictSnapshot{keys: make([]string, len(old.keys), len(old.keys)+1), ids: make(map[string]uint32, len(old.ids)+1)}
	copy(next.keys, old.keys)
	for k, id := range old.ids {
		next.ids[k] = id
	}
	id := uint32(len(next.keys) + 1)
	next.keys = append(next.keys, k)
	next.ids[k] = id
	d.snapshot.Store(next)
	return id
}
func (d *Dictionary) Lookup(k string) (uint32, bool) {
	s := d.read()
	if s == nil {
		return 0, false
	}
	id := s.ids[k]
	return id, id != 0
}
func (d *Dictionary) Get(id uint32) (string, bool) {
	s := d.read()
	if s == nil || id == 0 || int(id) > len(s.keys) {
		return "", false
	}
	return s.keys[id-1], true
}
func (d *Dictionary) MarshalBinary() ([]byte, error) {
	s := d.read()
	if s == nil {
		return []byte{0}, nil
	}
	b := binary.AppendUvarint(nil, uint64(len(s.keys)))
	for _, k := range s.keys {
		b = binary.AppendUvarint(b, uint64(len(k)))
		b = append(b, k...)
	}
	return b, nil
}
func ParseDictionary(data []byte) (*Dictionary, error) {
	return ParseDictionaryWithLimits(data, Limits{})
}
func ParseDictionaryWithLimits(data []byte, limits Limits) (*Dictionary, error) {
	limits = limits.normalized()
	if len(data) > limits.MaxBytes {
		return nil, ErrLimit
	}
	r := NewReaderWithLimits(data, nil, limits)
	cnt, err := r.readVar()
	if err != nil || cnt > uint64(limits.MaxContainerEntries) || cnt > uint64(len(data)) {
		return nil, ErrInvalid
	}
	keys := make([]string, 0, int(cnt))
	seen := make(map[string]struct{}, int(cnt))
	for i := uint64(0); i < cnt; i++ {
		n, e := r.readVar()
		if e != nil {
			return nil, e
		}
		if n > uint64(limits.MaxKeyBytes) {
			return nil, ErrLimit
		}
		b, e := r.readBytes(n)
		if e != nil || !utf8.Valid(b) {
			return nil, ErrInvalid
		}
		k := string(b)
		if _, found := seen[k]; found {
			return nil, ErrInvalid
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	if r.Remaining() != 0 {
		return nil, ErrInvalid
	}
	return NewDictionary(keys...), nil
}
