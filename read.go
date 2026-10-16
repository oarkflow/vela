package vela

import (
	"bytes"
	"encoding/binary"
	"math"
	"unicode/utf8"
)

// Reader operates on a borrowed buffer. Do not mutate/reuse it while a View is alive.
type Reader struct {
	data   []byte
	pos    int
	dict   *Dictionary
	limits Limits
	nodes  int
}

func NewReader(data []byte, d *Dictionary) *Reader { return NewReaderWithLimits(data, d, Limits{}) }
func NewReaderWithLimits(data []byte, d *Dictionary, limits Limits) *Reader {
	return &Reader{data: data, dict: d, limits: limits.normalized()}
}
func (r *Reader) checkValue(depth int) error {
	if depth > r.limits.MaxDepth || len(r.data) > r.limits.MaxBytes {
		return ErrLimit
	}
	r.nodes++
	if r.nodes > r.limits.MaxNodes {
		return ErrLimit
	}
	return nil
}
func (r *Reader) Remaining() int { return len(r.data) - r.pos }
func (r *Reader) Position() int  { return r.pos }
func (r *Reader) readVar() (uint64, error) {
	if r.Remaining() <= 0 {
		return 0, ErrInvalid
	}
	n, k := binary.Uvarint(r.data[r.pos:])
	if k <= 0 {
		return 0, ErrInvalid
	}
	// Reject noncanonical varints, important for unique representation / hashing.
	if k > 1 && r.data[r.pos+k-1] == 0 {
		return 0, ErrInvalid
	}
	r.pos += k
	return n, nil
}
func (r *Reader) readHead() (byte, uint64, error) {
	if r.Remaining() == 0 {
		return 0, 0, ErrInvalid
	}
	b := r.data[r.pos]
	r.pos++
	kind := b >> 5
	size := uint64(b & 31)
	if size == 31 {
		n, err := r.readVar()
		if err == nil && n < 31 {
			return 0, 0, ErrInvalid
		}
		return kind, n, err
	}
	return kind, size, nil
}
func (r *Reader) readBytes(n uint64) ([]byte, error) {
	if n > uint64(r.Remaining()) {
		return nil, ErrInvalid
	}
	s := r.data[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return s, nil
}
func (r *Reader) key() (string, error) {
	t, err := r.readVar()
	if err != nil {
		return "", err
	}
	if t&1 == 0 {
		if t == 0 || r.dict == nil || t>>1 > 1<<20 {
			return "", ErrDictionary
		}
		s, ok := r.dict.Get(uint32(t >> 1))
		if !ok {
			return "", ErrDictionary
		}
		return s, nil
	}
	if t>>1 > uint64(r.limits.MaxKeyBytes) {
		return "", ErrLimit
	}
	b, err := r.readBytes(t >> 1)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", ErrInvalid
	}
	return string(b), nil
}
func (r *Reader) skipKey() error {
	t, err := r.readVar()
	if err != nil {
		return err
	}
	if t&1 == 0 {
		if t == 0 || r.dict == nil || t>>1 > 1<<20 {
			return ErrDictionary
		}
		_, ok := r.dict.Get(uint32(t >> 1))
		if !ok || t>>1 > 1<<20 {
			return ErrDictionary
		}
		return nil
	}
	if t>>1 > uint64(r.limits.MaxKeyBytes) {
		return ErrLimit
	}
	b, err := r.readBytes(t >> 1)
	if err != nil {
		return err
	}
	if !utf8.Valid(b) {
		return ErrInvalid
	}
	return nil
}
func (r *Reader) matchKey(key string) (bool, error) {
	t, err := r.readVar()
	if err != nil {
		return false, err
	}
	if t&1 == 0 {
		if t == 0 || r.dict == nil || t>>1 > 1<<20 {
			return false, ErrDictionary
		}
		s, ok := r.dict.Get(uint32(t >> 1))
		if !ok {
			return false, ErrDictionary
		}
		return s == key, nil
	}
	if t>>1 > uint64(r.limits.MaxKeyBytes) {
		return false, ErrLimit
	}
	b, err := r.readBytes(t >> 1)
	if err != nil {
		return false, err
	}
	if !utf8.Valid(b) {
		return false, ErrInvalid
	}
	return bytes.Equal(b, []byte(key)), nil
}
func (r *Reader) skip(depth int) error {
	if err := r.checkValue(depth); err != nil {
		return err
	}
	typ, n, err := r.readHead()
	if err != nil {
		return err
	}
	switch typ {
	case KindUint, KindInt:
		return nil
	case KindSpecial:
		if n > 4 {
			return ErrInvalid
		}
		switch byte(n) {
		case 0, 1, 2:
			return nil
		case 3:
			_, err = r.readBytes(4)
			return err
		case 4:
			_, err = r.readBytes(8)
			return err
		default:
			return ErrInvalid
		}
	case KindBytes:
		if n > uint64(r.limits.MaxStringBytes) {
			return ErrLimit
		}
		_, err = r.readBytes(n)
		return err
	case KindString:
		if n > uint64(r.limits.MaxStringBytes) {
			return ErrLimit
		}
		data, e := r.readBytes(n)
		if e != nil {
			return e
		}
		if !utf8.Valid(data) {
			return ErrInvalid
		}
		return nil
	case KindArray:
		if n > uint64(r.limits.MaxContainerEntries) || n > uint64(r.limits.MaxNodes-r.nodes) {
			return ErrLimit
		}
		if n > uint64(r.Remaining()) {
			return ErrInvalid
		}
		for i := uint64(0); i < n; i++ {
			if err = r.skip(depth + 1); err != nil {
				return err
			}
		}
		return nil
	case KindMap:
		if n > uint64(r.limits.MaxContainerEntries) || n > uint64(r.limits.MaxNodes-r.nodes) {
			return ErrLimit
		}
		if n > uint64(r.Remaining()/2) {
			return ErrInvalid
		}
		for i := uint64(0); i < n; i++ {
			if err = r.skipKey(); err != nil {
				return err
			}
			if err = r.skip(depth + 1); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrInvalid
	}
}
func (r *Reader) value(depth int) (any, error) {
	if err := r.checkValue(depth); err != nil {
		return nil, err
	}
	typ, n, err := r.readHead()
	if err != nil {
		return nil, err
	}
	switch typ {
	case KindUint:
		return n, nil
	case KindInt:
		return unzz(n), nil
	case KindSpecial:
		if n > 4 {
			return nil, ErrInvalid
		}
		switch byte(n) {
		case 0:
			return nil, nil
		case 1:
			return false, nil
		case 2:
			return true, nil
		case 3:
			b, e := r.readBytes(4)
			if e != nil {
				return nil, e
			}
			return math.Float32frombits(binary.LittleEndian.Uint32(b)), nil
		case 4:
			b, e := r.readBytes(8)
			if e != nil {
				return nil, e
			}
			return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
		}
		return nil, ErrInvalid
	case KindBytes:
		if n > uint64(r.limits.MaxStringBytes) {
			return nil, ErrLimit
		}
		b, e := r.readBytes(n)
		if e != nil {
			return nil, e
		}
		return append(make([]byte, 0, len(b)), b...), nil
	case KindString:
		if n > uint64(r.limits.MaxStringBytes) {
			return nil, ErrLimit
		}
		b, e := r.readBytes(n)
		if e != nil {
			return nil, e
		}
		if !utf8.Valid(b) {
			return nil, ErrInvalid
		}
		return string(b), nil
	case KindArray:
		if n > uint64(r.limits.MaxContainerEntries) || n > uint64(r.limits.MaxNodes-r.nodes) {
			return nil, ErrLimit
		}
		if n > uint64(r.Remaining()) {
			return nil, ErrInvalid
		}
		out := make([]any, 0, int(n))
		for i := uint64(0); i < n; i++ {
			v, e := r.value(depth + 1)
			if e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		return out, nil
	case KindMap:
		if n > uint64(r.limits.MaxContainerEntries) || n > uint64(r.limits.MaxNodes-r.nodes) {
			return nil, ErrLimit
		}
		if n > uint64(r.Remaining()/2) {
			return nil, ErrInvalid
		}
		out := make(map[string]any, int(n))
		for i := uint64(0); i < n; i++ {
			k, e := r.key()
			if e != nil {
				return nil, e
			}
			v, e := r.value(depth + 1)
			if e != nil {
				return nil, e
			}
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
		return out, nil
	default:
		return nil, ErrInvalid
	}
}
func Unmarshal(data []byte) (any, error) { return UnmarshalWithLimits(data, nil, Limits{}) }
func UnmarshalWithDictionary(data []byte, d *Dictionary) (any, error) {
	return UnmarshalWithLimits(data, d, Limits{})
}
func UnmarshalWithLimits(data []byte, d *Dictionary, limits Limits) (any, error) {
	r := Reader{data: data, dict: d, limits: limits.normalized()}
	v, err := r.value(0)
	if err != nil {
		return nil, err
	}
	if r.Remaining() != 0 {
		return nil, ErrInvalid
	}
	return v, nil
}

// Open validates the complete value and retains only a borrowed byte slice.
func Open(data []byte) (View, error) { return OpenWithDictionary(data, nil) }
func OpenWithDictionary(data []byte, d *Dictionary) (View, error) {
	return OpenWithLimits(data, d, Limits{})
}
func OpenWithLimits(data []byte, d *Dictionary, limits Limits) (View, error) {
	r := Reader{data: data, dict: d, limits: limits.normalized()}
	if err := r.skip(0); err != nil {
		return View{}, err
	}
	if r.Remaining() != 0 {
		return View{}, ErrInvalid
	}
	return View{data: data, dict: d, limits: r.limits}, nil
}

// View avoids materializing entire values; Get scans map keys and skips unrelated values.
type View struct {
	data   []byte
	dict   *Dictionary
	limits Limits
}

func (v View) Bytes() []byte           { return v.data } // borrowed, do not mutate
func (v View) Unmarshal() (any, error) { return UnmarshalWithLimits(v.data, v.dict, v.limits) }
func (v View) Get(key string) (View, bool, error) {
	r := Reader{data: v.data, dict: v.dict, limits: v.limits.normalized()}
	typ, n, e := r.readHead()
	if e != nil {
		return View{}, false, e
	}
	if typ != KindMap {
		return View{}, false, ErrType
	}
	for i := uint64(0); i < n; i++ {
		matched, e := r.matchKey(key)
		if e != nil {
			return View{}, false, e
		}
		start := r.pos
		if e = r.skip(1); e != nil {
			return View{}, false, e
		}
		if matched {
			return View{data: r.data[start:r.pos], dict: v.dict, limits: v.limits}, true, nil
		}
	}
	return View{}, false, nil
}
func (v View) At(index int) (View, bool, error) {
	r := Reader{data: v.data, dict: v.dict, limits: v.limits.normalized()}
	typ, n, e := r.readHead()
	if e != nil {
		return View{}, false, e
	}
	if typ != KindArray {
		return View{}, false, ErrType
	}
	if index < 0 || uint64(index) >= n {
		return View{}, false, nil
	}
	for i := 0; i <= index; i++ {
		start := r.pos
		if e = r.skip(1); e != nil {
			return View{}, false, e
		}
		if i == index {
			return View{data: r.data[start:r.pos], dict: v.dict, limits: v.limits}, true, nil
		}
	}
	return View{}, false, nil
}
func (v View) Find(keys ...string) (View, bool, error) {
	curr := v
	for _, k := range keys {
		next, ok, e := curr.Get(k)
		if e != nil || !ok {
			return View{}, ok, e
		}
		curr = next
	}
	return curr, true, nil
}
func (v View) Uint() (uint64, error) {
	r := Reader{data: v.data, dict: v.dict, limits: v.limits.normalized()}
	k, n, e := r.readHead()
	if e != nil {
		return 0, e
	}
	if k != KindUint {
		return 0, ErrType
	}
	return n, nil
}
func (v View) Int() (int64, error) {
	r := Reader{data: v.data, dict: v.dict, limits: v.limits.normalized()}
	k, n, e := r.readHead()
	if e != nil {
		return 0, e
	}
	if k != KindInt {
		return 0, ErrType
	}
	return unzz(n), nil
}
func (v View) StringBytes() ([]byte, error) {
	r := Reader{data: v.data, dict: v.dict, limits: v.limits.normalized()}
	k, n, e := r.readHead()
	if e != nil {
		return nil, e
	}
	if k != KindString {
		return nil, ErrType
	}
	b, e := r.readBytes(n)
	if e != nil {
		return nil, e
	}
	return b, nil
}
func (v View) String() (string, error) {
	b, e := v.StringBytes()
	if e != nil {
		return "", e
	}
	return string(b), nil
}
func (v View) Bool() (bool, error) {
	if len(v.data) != 1 {
		return false, ErrType
	}
	switch v.data[0] {
	case True:
		return true, nil
	case False:
		return false, nil
	}
	return false, ErrType
}
func (v View) GetUint(k string) (uint64, bool, error) {
	f, ok, e := v.Get(k)
	if e != nil || !ok {
		return 0, ok, e
	}
	n, e := f.Uint()
	return n, true, e
}
func (v View) GetString(k string) (string, bool, error) {
	f, ok, e := v.Get(k)
	if e != nil || !ok {
		return "", ok, e
	}
	s, e := f.String()
	return s, true, e
}
func (v View) Equal(other View) bool {
	a, e := v.Unmarshal()
	if e != nil {
		return false
	}
	b, e := other.Unmarshal()
	if e != nil {
		return false
	}
	return equalValue(a, b)
}

// ValueSize returns the length of the first complete encoded value in data.
// This is useful for processing concatenated column values without decoding.
func ValueSize(data []byte, d *Dictionary) (int, error) {
	r := Reader{data: data, dict: d, limits: DefaultLimits}
	if err := r.skip(0); err != nil {
		return 0, err
	}
	return r.pos, nil
}
