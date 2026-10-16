// Package batch implements adaptive column-wise packing of schema-less records.
package batch

import (
	"encoding/binary"
	"errors"
	"math/bits"
	"reflect"
	"sort"
	"unicode/utf8"

	"github.com/oarkflow/vela"
)

const (
	modeRaw byte = iota
	modeBool
	modeUintPack
	modeStringDict
)

var ErrBatch = errors.New("vela/batch: corrupt or invalid batch")

type column struct {
	name       string
	mode       byte
	data       []byte
	dict       []string
	rawOffsets []int
	min        uint64
	bits       uint8
	bitmapLen  int
}
type Batch struct {
	rows    int
	columns []column
	byName  map[string]int
}

func (b *Batch) Len() int { return b.rows }
func (b *Batch) Columns() []string {
	r := make([]string, len(b.columns))
	for i, c := range b.columns {
		r[i] = c.name
	}
	return r
}
func uintOf(v any) (uint64, bool) {
	switch x := v.(type) {
	case uint:
		return uint64(x), true
	case uint8:
		return uint64(x), true
	case uint16:
		return uint64(x), true
	case uint32:
		return uint64(x), true
	case uint64:
		return x, true
	case int:
		if x >= 0 {
			return uint64(x), true
		}
	case int64:
		if x >= 0 {
			return uint64(x), true
		}
	}
	return 0, false
}

// Bit packing uses at most nine byte operations per 64-bit value, rather
// than looping over each bit. All inputs are bounds-checked by the caller.
func packedPut(data []byte, i int, width uint8, value uint64) {
	bit := uint64(i) * uint64(width)
	remaining := int(width)
	for remaining > 0 {
		shift := uint(bit & 7)
		take := min(8-int(shift), remaining)
		mask := uint64((1 << uint(take)) - 1)
		data[bit>>3] |= byte((value & mask) << shift)
		value >>= uint(take)
		bit += uint64(take)
		remaining -= take
	}
}
func packedGet(data []byte, idx int, width uint8) uint64 {
	bit := uint64(idx) * uint64(width)
	remaining := int(width)
	var result uint64
	outShift := uint(0)
	for remaining > 0 {
		shift := uint(bit & 7)
		take := min(8-int(shift), remaining)
		mask := uint64((1 << uint(take)) - 1)
		result |= (uint64(data[bit>>3]) >> shift & mask) << outShift
		outShift += uint(take)
		bit += uint64(take)
		remaining -= take
	}
	return result
}
func bitAt(d []byte, i int) bool { return d[i/8]&(1<<uint(i%8)) != 0 }
func setBit(d []byte, i int)     { d[i/8] |= 1 << uint(i%8) }

// Marshal creates a self-contained batch. Only codecs which beat raw column
// byte size are chosen; this is a size heuristic, not a CPU throughput promise.
func Marshal(rows []map[string]any) ([]byte, error) { return marshal(rows, false) }

// MarshalFast avoids evaluating raw fallback bytes for columns that have a
// specialized encoding. It prioritizes encoder CPU over guaranteed byte-minimum.
func MarshalFast(rows []map[string]any) ([]byte, error) { return marshal(rows, true) }
func marshal(rows []map[string]any, fast bool) ([]byte, error) {
	if len(rows) > DefaultLimits.MaxRows {
		return nil, vela.ErrLimit
	}
	keyset := make(map[string]struct{})
	for _, row := range rows {
		for k := range row {
			if !utf8.ValidString(k) || len(k) > DefaultLimits.MaxFieldBytes {
				return nil, ErrBatch
			}
			keyset[k] = struct{}{}
			if len(keyset) > DefaultLimits.MaxColumns {
				return nil, vela.ErrLimit
			}
		}
	}
	keys := make([]string, 0, len(keyset))
	for k := range keyset {
		keys = append(keys, k)
	}
	if uint64(len(rows))*uint64(len(keys)) > uint64(DefaultLimits.MaxCells) {
		return nil, vela.ErrLimit
	}
	sort.Strings(keys)
	out := make([]byte, 0, 64+len(keys)*16)
	out = append(out, 'V', 'L', 'B', '1')
	out = binary.AppendUvarint(out, uint64(len(rows)))
	out = binary.AppendUvarint(out, uint64(len(keys)))
	for _, key := range keys {
		allBool, allUint, allString := len(rows) > 0, len(rows) > 0, len(rows) > 0
		var minVal, maxVal uint64
		hasUInt := false
		var raw []byte
		if !fast {
			raw = make([]byte, 0, len(rows)*3)
		}
		// Pass 1: inspect types/extent and optionally build raw fallback.
		for _, row := range rows {
			v := row[key]
			if !fast {
				var err error
				raw, err = vela.Append(raw, v)
				if err != nil {
					return nil, err
				}
			}
			if v != nil {
				if _, ok := v.(bool); !ok {
					allBool = false
				}
				if _, ok := v.(string); !ok {
					allString = false
				}
			}
			if n, ok := uintOf(v); ok {
				if !hasUInt || n < minVal {
					minVal = n
				}
				if !hasUInt || n > maxVal {
					maxVal = n
				}
				hasUInt = true
			} else {
				allUint = false
			}
		}
		bestMode := modeRaw
		best := raw
		if allBool {
			n := (len(rows) + 7) / 8
			encoded := make([]byte, n*2)
			for i, row := range rows {
				v := row[key]
				if v != nil {
					setBit(encoded[:n], i)
					if v.(bool) {
						setBit(encoded[n:], i)
					}
				}
			}
			if fast || len(encoded) < len(best) {
				bestMode, best = modeBool, encoded
			}
		} else if allUint && hasUInt {
			width := uint8(bits.Len64(maxVal - minVal))
			encoded := binary.AppendUvarint(nil, minVal)
			encoded = append(encoded, width)
			start := len(encoded)
			encoded = append(encoded, make([]byte, (uint64(len(rows))*uint64(width)+7)/8)...)
			for i, row := range rows {
				n, _ := uintOf(row[key])
				packedPut(encoded[start:], i, width, n-minVal)
			}
			if fast || len(encoded) < len(best) {
				bestMode, best = modeUintPack, encoded
			}
		} else if allString {
			ids := make(map[string]uint64)
			dict := make([]string, 0, 16)
			for _, row := range rows {
				if v := row[key]; v != nil {
					str := v.(string)
					if ids[str] == 0 {
						id := uint64(len(dict) + 1)
						ids[str] = id
						dict = append(dict, str)
					}
				}
			}
			if len(dict) > DefaultLimits.MaxDictEntries {
				return nil, vela.ErrLimit
			}
			encoded := binary.AppendUvarint(nil, uint64(len(dict)))
			for _, str := range dict {
				encoded = binary.AppendUvarint(encoded, uint64(len(str)))
				encoded = append(encoded, str...)
			}
			width := uint8(bits.Len64(uint64(len(dict))))
			encoded = append(encoded, width)
			start := len(encoded)
			encoded = append(encoded, make([]byte, (uint64(len(rows))*uint64(width)+7)/8)...)
			for i, row := range rows {
				if v := row[key]; v != nil {
					packedPut(encoded[start:], i, width, ids[v.(string)])
				}
			}
			if fast || len(encoded) < len(best) {
				bestMode, best = modeStringDict, encoded
			}
		}
		if fast && bestMode == modeRaw {
			for _, row := range rows {
				var err error
				raw, err = vela.Append(raw, row[key])
				if err != nil {
					return nil, err
				}
			}
			best = raw
		}
		out = binary.AppendUvarint(out, uint64(len(key)))
		out = append(out, key...)
		out = append(out, bestMode)
		out = binary.AppendUvarint(out, uint64(len(best)))
		out = append(out, best...)
		if len(out) > DefaultLimits.MaxBytes {
			return nil, vela.ErrLimit
		}
	}
	return out, nil
}
func takeVar(d []byte, p *int) (uint64, error) {
	if *p >= len(d) {
		return 0, ErrBatch
	}
	x, n := binary.Uvarint(d[*p:])
	if n <= 0 || (n > 1 && d[*p+n-1] == 0) {
		return 0, ErrBatch
	}
	*p += n
	return x, nil
}
func take(d []byte, p *int, n uint64) ([]byte, error) {
	if n > uint64(len(d)-*p) {
		return nil, ErrBatch
	}
	b := d[*p : *p+int(n)]
	*p += int(n)
	return b, nil
}

// Limits bounds input allocation amplification and processing.
type Limits struct{ MaxBytes, MaxRows, MaxColumns, MaxFieldBytes, MaxDictEntries, MaxCells int }

var DefaultLimits = Limits{MaxBytes: 64 << 20, MaxRows: 1 << 20, MaxColumns: 4096, MaxFieldBytes: 1 << 20, MaxDictEntries: 1 << 20, MaxCells: 8 << 20}

func (l Limits) normalized() Limits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultLimits.MaxBytes
	}
	if l.MaxRows <= 0 {
		l.MaxRows = DefaultLimits.MaxRows
	}
	if l.MaxColumns <= 0 {
		l.MaxColumns = DefaultLimits.MaxColumns
	}
	if l.MaxFieldBytes <= 0 {
		l.MaxFieldBytes = DefaultLimits.MaxFieldBytes
	}
	if l.MaxDictEntries <= 0 {
		l.MaxDictEntries = DefaultLimits.MaxDictEntries
	}
	if l.MaxCells <= 0 {
		l.MaxCells = DefaultLimits.MaxCells
	}
	return l
}
func Open(data []byte) (*Batch, error) { return OpenWithLimits(data, Limits{}) }
func OpenWithLimits(data []byte, limits Limits) (*Batch, error) {
	limits = limits.normalized()
	if len(data) > limits.MaxBytes {
		return nil, vela.ErrLimit
	}
	if len(data) < 4 || string(data[:4]) != "VLB1" {
		return nil, ErrBatch
	}
	p := 4
	nr, e := takeVar(data, &p)
	if e != nil || nr > uint64(limits.MaxRows) {
		return nil, ErrBatch
	}
	nc, e := takeVar(data, &p)
	if e != nil || nc > uint64(limits.MaxColumns) || nr*nc > uint64(limits.MaxCells) {
		return nil, ErrBatch
	}
	b := &Batch{rows: int(nr), columns: make([]column, 0, int(nc)), byName: make(map[string]int, int(nc))}
	for i := uint64(0); i < nc; i++ {
		n, e := takeVar(data, &p)
		if e != nil {
			return nil, e
		}
		if n > uint64(limits.MaxFieldBytes) {
			return nil, vela.ErrLimit
		}
		k, e := take(data, &p, n)
		if e != nil || !utf8.Valid(k) {
			return nil, ErrBatch
		}
		if p >= len(data) {
			return nil, ErrBatch
		}
		mode := data[p]
		p++
		n, e = takeVar(data, &p)
		if e != nil {
			return nil, e
		}
		payload, e := take(data, &p, n)
		if e != nil {
			return nil, e
		}
		name := string(k)
		if _, exists := b.byName[name]; exists {
			return nil, ErrBatch
		}
		c := column{name: name, mode: mode, data: payload}
		switch mode {
		case modeRaw:
			if b.rows > len(payload) {
				return nil, ErrBatch
			}
			c.rawOffsets = make([]int, 0, b.rows+1)
			pos := 0
			for j := 0; j < b.rows; j++ {
				c.rawOffsets = append(c.rawOffsets, pos)
				sz, e := vela.ValueSize(payload[pos:], nil)
				if e != nil {
					return nil, ErrBatch
				}
				pos += sz
			}
			if pos != len(payload) {
				return nil, ErrBatch
			}
			c.rawOffsets = append(c.rawOffsets, pos)
		case modeBool:
			c.bitmapLen = (b.rows + 7) / 8
			if len(payload) != c.bitmapLen*2 {
				return nil, ErrBatch
			}
		case modeUintPack:
			bp := 0
			base, e := takeVar(payload, &bp)
			if e != nil || bp >= len(payload) {
				return nil, ErrBatch
			}
			c.min = base
			c.bits = payload[bp]
			bp++
			if c.bits > 64 || uint64(len(payload)-bp) != (uint64(b.rows)*uint64(c.bits)+7)/8 {
				return nil, ErrBatch
			}
			c.data = payload[bp:]
			// If the maximum representable delta fits, every packed value is safe.
			// Otherwise inspect each value to reject arithmetic overflow.
			if c.bits == 64 || (c.bits > 0 && c.min > ^uint64(0)-((uint64(1)<<c.bits)-1)) {
				for j := 0; j < b.rows; j++ {
					if packedGet(c.data, j, c.bits) > ^uint64(0)-c.min {
						return nil, ErrBatch
					}
				}
			}
		case modeStringDict:
			bp := 0
			n, e := takeVar(payload, &bp)
			if e != nil || n > uint64(len(payload)) || n > uint64(limits.MaxDictEntries) {
				return nil, ErrBatch
			}
			c.dict = make([]string, 0, int(n))
			seenDict := make(map[string]struct{}, int(n))
			for j := uint64(0); j < n; j++ {
				sz, e := takeVar(payload, &bp)
				if e != nil {
					return nil, ErrBatch
				}
				if sz > uint64(limits.MaxFieldBytes) {
					return nil, vela.ErrLimit
				}
				s, e := take(payload, &bp, sz)
				if e != nil || !utf8.Valid(s) {
					return nil, ErrBatch
				}
				str := string(s)
				if _, ok := seenDict[str]; ok {
					return nil, ErrBatch
				}
				seenDict[str] = struct{}{}
				c.dict = append(c.dict, str)
			}
			if bp >= len(payload) {
				return nil, ErrBatch
			}
			c.bits = payload[bp]
			bp++
			if c.bits > 64 || uint64(len(payload)-bp) != (uint64(b.rows)*uint64(c.bits)+7)/8 {
				return nil, ErrBatch
			}
			c.data = payload[bp:]
			// No per-row validation when all representable IDs are valid.
			if c.bits == 64 || (c.bits > 0 && ((uint64(1)<<c.bits)-1) > uint64(len(c.dict))) {
				for j := 0; j < b.rows; j++ {
					if packedGet(c.data, j, c.bits) > uint64(len(c.dict)) {
						return nil, ErrBatch
					}
				}
			}
		default:
			return nil, ErrBatch
		}
		b.byName[name] = len(b.columns)
		b.columns = append(b.columns, c)
	}
	if p != len(data) {
		return nil, ErrBatch
	}
	return b, nil
}
func (b *Batch) getColumn(c *column, i int) (any, error) {
	if i < 0 || i >= b.rows {
		return nil, ErrBatch
	}
	switch c.mode {
	case modeBool:
		if !bitAt(c.data, i) {
			return nil, nil
		}
		return bitAt(c.data[c.bitmapLen:], i), nil
	case modeUintPack:
		return c.min + packedGet(c.data, i, c.bits), nil
	case modeStringDict:
		id := packedGet(c.data, i, c.bits)
		if id == 0 {
			return nil, nil
		}
		if id > uint64(len(c.dict)) {
			return nil, ErrBatch
		}
		return c.dict[id-1], nil
	case modeRaw:
		start, end := c.rawOffsets[i], c.rawOffsets[i+1]
		return vela.Unmarshal(c.data[start:end])
	}
	return nil, ErrBatch
}
func (b *Batch) Get(row int, name string) (any, bool, error) {
	i, ok := b.byName[name]
	if !ok {
		return nil, false, nil
	}
	v, e := b.getColumn(&b.columns[i], row)
	return v, true, e
}
func (b *Batch) Project(row int, fields ...string) (map[string]any, error) {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		v, ok, e := b.Get(row, f)
		if e != nil {
			return nil, e
		}
		if ok {
			out[f] = v
		}
	}
	return out, nil
}

// FilterEq matches column values. Only the requested column is decoded.
func (b *Batch) FilterEq(name string, want any) ([]int, error) {
	i, ok := b.byName[name]
	if !ok {
		return nil, nil
	}
	c := &b.columns[i]
	out := make([]int, 0, min(b.rows/2+1, 1024))
	switch c.mode {
	case modeStringDict:
		var target uint64
		if want != nil {
			s, ok := want.(string)
			if !ok {
				return out, nil
			}
			for j, v := range c.dict {
				if s == v {
					target = uint64(j + 1)
					break
				}
			}
			if target == 0 {
				return out, nil
			}
		}
		for j := 0; j < b.rows; j++ {
			if packedGet(c.data, j, c.bits) == target {
				out = append(out, j)
			}
		}
	case modeUintPack:
		n, ok := want.(uint64)
		if !ok {
			return out, nil
		}
		if n < c.min {
			return out, nil
		}
		delta := n - c.min
		for j := 0; j < b.rows; j++ {
			if packedGet(c.data, j, c.bits) == delta {
				out = append(out, j)
			}
		}
	case modeBool:
		target, typed := want.(bool)
		for j := 0; j < b.rows; j++ {
			exists := bitAt(c.data, j)
			if want == nil && !exists {
				out = append(out, j)
			} else if typed && exists && bitAt(c.data[c.bitmapLen:], j) == target {
				out = append(out, j)
			}
		}
	default:
		for j := 0; j < b.rows; j++ {
			v, e := b.getColumn(c, j)
			if e != nil {
				return nil, e
			}
			if simpleEqual(v, want) {
				out = append(out, j)
			}
		}
	}
	return out, nil
}

// Simple scalar comparison avoids reflective allocations on hot scan paths.
func simpleEqual(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case uint64:
		y, ok := b.(uint64)
		return ok && x == y
	case int64:
		y, ok := b.(int64)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case nil:
		return b == nil
	default:
		return reflect.DeepEqual(a, b)
	}
}
