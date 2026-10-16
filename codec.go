// Package vela implements a compact, schema-less binary value codec.
// Wire v1: high 3 tag bits are kind, low 5 contain inline uint/length/count
// 0..30, or escape 31 followed by an unsigned LEB128 of the actual value.
// Maps contain count entries; each key uses a uvarint token: odd means
// inline UTF-8 key (token>>1 bytes); even means dictionary ID (token>>1).
package vela

import (
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"unicode/utf8"
)

const (
	KindUint byte = iota
	KindInt
	KindSpecial
	KindBytes
	KindString
	KindArray
	KindMap
	KindExtension
)
const (
	Null     byte = 0x40
	False    byte = 0x41
	True     byte = 0x42
	Float32  byte = 0x43
	Float64  byte = 0x44
	MaxDepth      = 64
)

var (
	ErrInvalid    = errors.New("vela: invalid or truncated data")
	ErrLimit      = errors.New("vela: length/depth limit exceeded")
	ErrType       = errors.New("vela: type mismatch")
	ErrDictionary = errors.New("vela: missing dictionary entry")
)

func appendUvarint(dst []byte, x uint64) []byte { return binary.AppendUvarint(dst, x) }
func head(dst []byte, kind byte, n uint64) []byte {
	if n < 31 {
		return append(dst, kind<<5|byte(n))
	}
	return appendUvarint(append(dst, kind<<5|31), n)
}
func zz(v int64) uint64   { return uint64(v<<1) ^ uint64(v>>63) }
func unzz(v uint64) int64 { return int64(v>>1) ^ -int64(v&1) }

// Append serializes a value into dst; passing a sufficiently large buffer
// lets callers reuse storage with no allocation for scalar/manual encoding.
func Append(dst []byte, value any) ([]byte, error) { return appendValue(dst, value, nil, 0) }
func Marshal(value any) ([]byte, error)            { return Append(make([]byte, 0, 64), value) }
func AppendWithDictionary(dst []byte, v any, d *Dictionary) ([]byte, error) {
	return appendValue(dst, v, d, 0)
}
func MarshalWithDictionary(v any, d *Dictionary) ([]byte, error) {
	return AppendWithDictionary(make([]byte, 0, 64), v, d)
}

func appendKey(dst []byte, s string, d *Dictionary) []byte {
	if d != nil {
		if id, ok := d.Lookup(s); ok {
			return appendUvarint(dst, uint64(id)<<1)
		}
	}
	dst = appendUvarint(dst, uint64(len(s))<<1|1)
	return append(dst, s...)
}
func appendValue(dst []byte, v any, d *Dictionary, depth int) ([]byte, error) {
	if depth > MaxDepth {
		return nil, ErrLimit
	}
	switch x := v.(type) {
	case nil:
		return append(dst, Null), nil
	case bool:
		if x {
			return append(dst, True), nil
		}
		return append(dst, False), nil
	case uint8:
		return head(dst, KindUint, uint64(x)), nil
	case uint16:
		return head(dst, KindUint, uint64(x)), nil
	case uint32:
		return head(dst, KindUint, uint64(x)), nil
	case uint64:
		return head(dst, KindUint, x), nil
	case uint:
		return head(dst, KindUint, uint64(x)), nil
	case int8:
		return head(dst, KindInt, zz(int64(x))), nil
	case int16:
		return head(dst, KindInt, zz(int64(x))), nil
	case int32:
		return head(dst, KindInt, zz(int64(x))), nil
	case int64:
		return head(dst, KindInt, zz(x)), nil
	case int:
		return head(dst, KindInt, zz(int64(x))), nil
	case float32:
		dst = append(dst, Float32)
		return binary.LittleEndian.AppendUint32(dst, math.Float32bits(x)), nil
	case float64:
		dst = append(dst, Float64)
		return binary.LittleEndian.AppendUint64(dst, math.Float64bits(x)), nil
	case string:
		if !utf8.ValidString(x) {
			return nil, fmt.Errorf("vela: invalid UTF-8 string")
		}
		dst = head(dst, KindString, uint64(len(x)))
		return append(dst, x...), nil
	case []byte:
		if x == nil {
			return append(dst, Null), nil
		}
		dst = head(dst, KindBytes, uint64(len(x)))
		return append(dst, x...), nil
	case []any:
		if x == nil {
			return append(dst, Null), nil
		}
		dst = head(dst, KindArray, uint64(len(x)))
		var err error
		for _, e := range x {
			dst, err = appendValue(dst, e, d, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil
	case map[string]any:
		if x == nil {
			return append(dst, Null), nil
		}
		dst = head(dst, KindMap, uint64(len(x)))
		var err error
		for k, e := range x {
			if !utf8.ValidString(k) {
				return nil, ErrInvalid
			}
			dst = appendKey(dst, k, d)
			dst, err = appendValue(dst, e, d, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil
	default:
		return appendReflect(dst, reflect.ValueOf(v), d, depth)
	}
}

// Reflection is an optional compatibility fallback for user structs, aliases,
// typed slices and typed maps; raw Writer and base types never use reflection.
func appendReflect(dst []byte, v reflect.Value, d *Dictionary, depth int) ([]byte, error) {
	if depth > MaxDepth {
		return nil, ErrLimit
	}
	if !v.IsValid() {
		return append(dst, Null), nil
	}
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return append(dst, Null), nil
		}
		if v.CanInterface() {
			if marshaler, ok := v.Interface().(encoding.TextMarshaler); ok {
				b, err := marshaler.MarshalText()
				if err != nil {
					return nil, err
				}
				return appendValue(dst, string(b), d, depth)
			}
		}
		v = v.Elem()
	}
	if v.CanInterface() {
		if marshaler, ok := v.Interface().(encoding.TextMarshaler); ok {
			b, err := marshaler.MarshalText()
			if err != nil {
				return nil, err
			}
			return appendValue(dst, string(b), d, depth)
		}
	}
	switch v.Kind() {
	case reflect.Bool:
		return appendValue(dst, v.Bool(), d, depth)
	case reflect.String:
		return appendValue(dst, v.String(), d, depth)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendValue(dst, v.Int(), d, depth)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return appendValue(dst, v.Uint(), d, depth)
	case reflect.Float32:
		return appendValue(dst, float32(v.Float()), d, depth)
	case reflect.Float64:
		return appendValue(dst, v.Float(), d, depth)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return append(dst, Null), nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if v.Kind() == reflect.Array {
				bytes := make([]byte, v.Len())
				for i := range bytes {
					bytes[i] = byte(v.Index(i).Uint())
				}
				return appendValue(dst, bytes, d, depth)
			}
			return appendValue(dst, v.Bytes(), d, depth)
		}
		dst = head(dst, KindArray, uint64(v.Len()))
		var err error
		for i := 0; i < v.Len(); i++ {
			dst, err = appendReflect(dst, v.Index(i), d, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("vela: map key must be string")
		}
		if v.IsNil() {
			return append(dst, Null), nil
		}
		keys := v.MapKeys()
		dst = head(dst, KindMap, uint64(len(keys)))
		var err error
		for _, k := range keys {
			s := k.String()
			if !utf8.ValidString(s) {
				return nil, ErrInvalid
			}
			dst = appendKey(dst, s, d)
			dst, err = appendReflect(dst, v.MapIndex(k), d, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil
	case reflect.Struct:
		fields := fieldsFor(v, true)
		dst = head(dst, KindMap, uint64(len(fields)))
		var err error
		for _, f := range fields {
			if !utf8.ValidString(f.name) {
				return nil, ErrInvalid
			}
			dst = appendKey(dst, f.name, d)
			dst, err = appendReflect(dst, v.Field(f.index), d, depth+1)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil
	default:
		return nil, fmt.Errorf("vela: unsupported Go type %s", v.Type())
	}
}
