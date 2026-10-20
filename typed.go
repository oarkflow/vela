package vela

import (
	"encoding"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"unicode/utf8"
)

// UnmarshalInto decodes a value into a non-nil pointer. Integer widths and
// container types are checked; unknown struct fields are ignored.
func UnmarshalInto(data []byte, dst any) error {
	return UnmarshalIntoWithLimits(data, nil, Limits{}, dst)
}

// UnmarshalIntoWithDictionary decodes dictionary keys using d.
func UnmarshalIntoWithDictionary(data []byte, d *Dictionary, dst any) error {
	return UnmarshalIntoWithLimits(data, d, Limits{}, dst)
}

// UnmarshalIntoWithLimits applies the same input bounds as UnmarshalWithLimits.
func UnmarshalIntoWithLimits(data []byte, d *Dictionary, limits Limits, dst any) error {
	output := reflect.ValueOf(dst)
	if !output.IsValid() || output.Kind() != reflect.Pointer || output.IsNil() {
		return fmt.Errorf("vela: destination must be a non-nil pointer: %w", ErrType)
	}
	// Decode into a temporary value so an error never leaves a partial result.
	tmp := reflect.New(output.Elem().Type()).Elem()
	r := Reader{data: data, dict: d, limits: limits.normalized()}
	if err := r.readTyped(tmp, 0); err != nil {
		return err
	}
	if r.Remaining() != 0 {
		return ErrInvalid
	}
	output.Elem().Set(tmp)
	return nil
}

func (r *Reader) readTyped(dst reflect.Value, depth int) error {
	if dst.Kind() == reflect.Pointer {
		if r.Remaining() == 0 {
			return ErrInvalid
		}
		if r.data[r.pos] == Null {
			if err := r.checkValue(depth); err != nil {
				return err
			}
			r.pos++
			dst.SetZero()
			return nil
		}
		p := reflect.New(dst.Type().Elem())
		if err := r.readTyped(p.Elem(), depth); err != nil {
			return err
		}
		dst.Set(p)
		return nil
	}
	// Interfaces and custom text values are uncommon; use the generic reader for
	// these so their own conversion rules stay in one place.
	if dst.Kind() == reflect.Interface || (dst.CanAddr() && implementsTextUnmarshaler(dst.Addr())) {
		v, err := r.value(depth)
		if err != nil {
			return err
		}
		return assignValue(dst, v)
	}
	if err := r.checkValue(depth); err != nil {
		return err
	}
	kind, n, err := r.readHead()
	if err != nil {
		return err
	}
	if kind == KindSpecial && n == 0 {
		dst.SetZero()
		return nil
	}
	switch dst.Kind() {
	case reflect.Bool:
		if kind != KindSpecial || (n != 1 && n != 2) {
			return ErrType
		}
		dst.SetBool(n == 2)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if kind != KindInt {
			return ErrType
		}
		v := unzz(n)
		if dst.OverflowInt(v) {
			return ErrType
		}
		dst.SetInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if kind != KindUint || dst.OverflowUint(n) {
			return ErrType
		}
		dst.SetUint(n)
	case reflect.Float32:
		if kind != KindSpecial || n != 3 {
			return ErrType
		}
		b, err := r.readBytes(4)
		if err != nil {
			return err
		}
		dst.SetFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))))
	case reflect.Float64:
		if kind != KindSpecial || (n != 3 && n != 4) {
			return ErrType
		}
		if n == 3 {
			b, err := r.readBytes(4)
			if err != nil {
				return err
			}
			dst.SetFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))))
		} else {
			b, err := r.readBytes(8)
			if err != nil {
				return err
			}
			dst.SetFloat(math.Float64frombits(binary.LittleEndian.Uint64(b)))
		}
	case reflect.String:
		if kind != KindString {
			return ErrType
		}
		if n > uint64(r.limits.MaxStringBytes) {
			return ErrLimit
		}
		b, err := r.readBytes(n)
		if err != nil {
			return err
		}
		if !utf8.Valid(b) {
			return ErrInvalid
		}
		dst.SetString(string(b))
	case reflect.Slice, reflect.Array:
		if dst.Type().Elem().Kind() == reflect.Uint8 {
			if kind != KindBytes {
				return ErrType
			}
			if n > uint64(r.limits.MaxStringBytes) {
				return ErrLimit
			}
			b, err := r.readBytes(n)
			if err != nil {
				return err
			}
			if dst.Kind() == reflect.Array {
				if len(b) != dst.Len() {
					return ErrType
				}
				reflect.Copy(dst, reflect.ValueOf(b))
			} else {
				dst.SetBytes(append(make([]byte, 0, len(b)), b...))
			}
			return nil
		}
		if kind != KindArray {
			return ErrType
		}
		if n > uint64(r.limits.MaxContainerEntries) || n > uint64(r.limits.MaxNodes-r.nodes) {
			return ErrLimit
		}
		if n > uint64(r.Remaining()) {
			return ErrInvalid
		}
		if dst.Kind() == reflect.Array {
			if n != uint64(dst.Len()) {
				return ErrType
			}
		} else {
			dst.Set(reflect.MakeSlice(dst.Type(), int(n), int(n)))
		}
		for i := 0; i < int(n); i++ {
			if err := r.readTyped(dst.Index(i), depth+1); err != nil {
				return fmt.Errorf("vela: index %d: %w", i, err)
			}
		}
	case reflect.Map:
		if kind != KindMap || dst.Type().Key().Kind() != reflect.String {
			return ErrType
		}
		if n > uint64(r.limits.MaxContainerEntries) || n > uint64(r.limits.MaxNodes-r.nodes) {
			return ErrLimit
		}
		if n > uint64(r.Remaining()/2) {
			return ErrInvalid
		}
		out := reflect.MakeMapWithSize(dst.Type(), int(n))
		for i := uint64(0); i < n; i++ {
			name, err := r.key()
			if err != nil {
				return err
			}
			key := reflect.ValueOf(name).Convert(dst.Type().Key())
			if out.MapIndex(key).IsValid() {
				if err := r.skip(depth + 1); err != nil {
					return err
				}
				continue
			}
			value := reflect.New(dst.Type().Elem()).Elem()
			if err := r.readTyped(value, depth+1); err != nil {
				return fmt.Errorf("vela: map key %q: %w", name, err)
			}
			out.SetMapIndex(key, value)
		}
		dst.Set(out)
	case reflect.Struct:
		if kind != KindMap {
			return ErrType
		}
		if n > uint64(r.limits.MaxContainerEntries) || n > uint64(r.limits.MaxNodes-r.nodes) {
			return ErrLimit
		}
		if n > uint64(r.Remaining()/2) {
			return ErrInvalid
		}
		fields := fieldsFor(dst, false)
		var seen uint64
		var extraSeen []uint64
		if len(fields) > 64 {
			extraSeen = make([]uint64, (len(fields)+63)/64)
		}
		for i := uint64(0); i < n; i++ {
			name, err := r.key()
			if err != nil {
				return err
			}
			index := -1
			for j, f := range fields {
				if f.name != name {
					continue
				}
				word, bit := j/64, uint64(1)<<uint(j%64)
				if extraSeen == nil {
					if seen&bit == 0 {
						seen |= bit
						index = f.index
					}
				} else if extraSeen[word]&bit == 0 {
					extraSeen[word] |= bit
					index = f.index
				}
				break
			}
			if index < 0 {
				if err := r.skip(depth + 1); err != nil {
					return err
				}
				continue
			}
			if err := r.readTyped(dst.Field(index), depth+1); err != nil {
				return fmt.Errorf("vela: field %q: %w", name, err)
			}
		}
	default:
		return fmt.Errorf("vela: unsupported destination %s: %w", dst.Type(), ErrType)
	}
	return nil
}

func implementsTextUnmarshaler(v reflect.Value) bool {
	_, ok := v.Interface().(encoding.TextUnmarshaler)
	return ok
}

func assignValue(dst reflect.Value, src any) error {
	if src == nil {
		dst.SetZero()
		return nil
	}
	if value := reflect.ValueOf(src); value.Type().AssignableTo(dst.Type()) {
		dst.Set(value)
		return nil
	}
	if dst.CanAddr() {
		if unmarshaler, ok := dst.Addr().Interface().(encoding.TextUnmarshaler); ok {
			s, ok := src.(string)
			if !ok {
				return ErrType
			}
			return unmarshaler.UnmarshalText([]byte(s))
		}
	}
	if dst.Kind() == reflect.Pointer {
		p := reflect.New(dst.Type().Elem())
		if err := assignValue(p.Elem(), src); err != nil {
			return err
		}
		dst.Set(p)
		return nil
	}
	if dst.Kind() == reflect.Interface {
		v := reflect.ValueOf(src)
		if !v.Type().AssignableTo(dst.Type()) {
			return ErrType
		}
		dst.Set(v)
		return nil
	}
	switch dst.Kind() {
	case reflect.Bool:
		v, ok := src.(bool)
		if !ok {
			return ErrType
		}
		dst.SetBool(v)
	case reflect.String:
		v, ok := src.(string)
		if !ok {
			return ErrType
		}
		dst.SetString(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, ok := src.(int64)
		if !ok || dst.OverflowInt(v) {
			return ErrType
		}
		dst.SetInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, ok := src.(uint64)
		if !ok || dst.OverflowUint(v) {
			return ErrType
		}
		dst.SetUint(v)
	case reflect.Float32, reflect.Float64:
		switch v := src.(type) {
		case float32:
			dst.SetFloat(float64(v))
		case float64:
			if dst.Kind() != reflect.Float64 {
				return ErrType
			}
			dst.SetFloat(v)
		default:
			return ErrType
		}
	case reflect.Slice:
		if dst.Type().Elem().Kind() == reflect.Uint8 {
			v, ok := src.([]byte)
			if !ok {
				return ErrType
			}
			dst.SetBytes(v)
			return nil
		}
		v, ok := src.([]any)
		if !ok {
			return ErrType
		}
		out := reflect.MakeSlice(dst.Type(), len(v), len(v))
		for i, element := range v {
			if err := assignValue(out.Index(i), element); err != nil {
				return fmt.Errorf("vela: slice index %d: %w", i, err)
			}
		}
		dst.Set(out)
	case reflect.Array:
		if dst.Type().Elem().Kind() == reflect.Uint8 {
			v, ok := src.([]byte)
			if !ok || len(v) != dst.Len() {
				return ErrType
			}
			reflect.Copy(dst, reflect.ValueOf(v))
			return nil
		}
		v, ok := src.([]any)
		if !ok || len(v) != dst.Len() {
			return ErrType
		}
		for i, element := range v {
			if err := assignValue(dst.Index(i), element); err != nil {
				return fmt.Errorf("vela: array index %d: %w", i, err)
			}
		}
	case reflect.Map:
		v, ok := src.(map[string]any)
		if !ok || dst.Type().Key().Kind() != reflect.String {
			return ErrType
		}
		out := reflect.MakeMapWithSize(dst.Type(), len(v))
		for name, element := range v {
			key := reflect.ValueOf(name).Convert(dst.Type().Key())
			value := reflect.New(dst.Type().Elem()).Elem()
			if err := assignValue(value, element); err != nil {
				return fmt.Errorf("vela: map key %q: %w", name, err)
			}
			out.SetMapIndex(key, value)
		}
		dst.Set(out)
	case reflect.Struct:
		v, ok := src.(map[string]any)
		if !ok {
			return ErrType
		}
		seen := make(map[string]bool, len(v))
		for _, f := range fieldsFor(dst, false) {
			value, exists := v[f.name]
			if !exists || seen[f.name] {
				continue
			}
			seen[f.name] = true
			if err := assignValue(dst.Field(f.index), value); err != nil {
				return fmt.Errorf("vela: field %q: %w", f.name, err)
			}
		}
	default:
		return fmt.Errorf("vela: unsupported destination %s: %w", dst.Type(), ErrType)
	}
	return nil
}
