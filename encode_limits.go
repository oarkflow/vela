package vela

import (
	"encoding"
	"reflect"
	"unicode/utf8"
)

// AppendLimited performs a bounded, allocation-light preflight before any
// encoded buffer growth. It is recommended for encoding untrusted values.
// The estimate intentionally overestimates small wire headers; a value close
// to the limit may be rejected even when its actual encoding would fit.
func AppendLimited(dst []byte, v any, d *Dictionary, limits Limits) ([]byte, error) {
	limits = limits.normalized()
	budget := &inputBudget{limits: limits, bytes: len(dst)}
	if err := budget.walk(reflect.ValueOf(v), 0); err != nil {
		return nil, err
	}
	var out []byte
	var err error
	if d == nil {
		out, err = Append(dst, v)
	} else {
		out, err = AppendWithDictionary(dst, v, d)
	}
	if err != nil {
		return nil, err
	}
	if len(out) > limits.MaxBytes {
		return nil, ErrLimit
	}
	return out, nil
}
func MarshalLimited(v any, d *Dictionary, limits Limits) ([]byte, error) {
	return AppendLimited(make([]byte, 0, 64), v, d, limits)
}

type inputBudget struct {
	limits Limits
	bytes  int
	nodes  int
}

func (b *inputBudget) add(n int) error {
	if n < 0 || n > b.limits.MaxBytes-b.bytes {
		return ErrLimit
	}
	b.bytes += n
	return nil
}
func (b *inputBudget) walk(v reflect.Value, depth int) error {
	if depth > b.limits.MaxDepth {
		return ErrLimit
	}
	b.nodes++
	if b.nodes > b.limits.MaxNodes {
		return ErrLimit
	}
	if err := b.add(11); err != nil {
		return err
	}
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return nil
		}
		if v.CanInterface() {
			if marshaler, ok := v.Interface().(encoding.TextMarshaler); ok {
				text, err := marshaler.MarshalText()
				if err != nil {
					return err
				}
				if len(text) > b.limits.MaxStringBytes || !utf8.Valid(text) {
					return ErrLimit
				}
				return b.add(len(text))
			}
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	if v.CanInterface() {
		if marshaler, ok := v.Interface().(encoding.TextMarshaler); ok {
			text, err := marshaler.MarshalText()
			if err != nil {
				return err
			}
			if len(text) > b.limits.MaxStringBytes || !utf8.Valid(text) {
				return ErrLimit
			}
			return b.add(len(text))
		}
	}
	switch v.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return nil
	case reflect.String:
		str := v.String()
		if len(str) > b.limits.MaxStringBytes || !utf8.ValidString(str) {
			return ErrLimit
		}
		return b.add(len(str))
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if v.Len() > b.limits.MaxStringBytes {
				return ErrLimit
			}
			return b.add(v.Len())
		}
		if v.Len() > b.limits.MaxContainerEntries {
			return ErrLimit
		}
		for i := 0; i < v.Len(); i++ {
			if err := b.walk(v.Index(i), depth+1); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		if v.Len() > b.limits.MaxContainerEntries {
			return ErrLimit
		}
		if v.Type().Key().Kind() != reflect.String {
			return ErrType
		}
		iter := v.MapRange()
		for iter.Next() {
			str := iter.Key().String()
			if len(str) > b.limits.MaxKeyBytes || !utf8.ValidString(str) {
				return ErrLimit
			}
			if err := b.add(len(str) + 10); err != nil {
				return err
			}
			if err := b.walk(iter.Value(), depth+1); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		fields := fieldsFor(v, true)
		if len(fields) > b.limits.MaxContainerEntries {
			return ErrLimit
		}
		for _, f := range fields {
			k := f.name
			if len(k) > b.limits.MaxKeyBytes || !utf8.ValidString(k) {
				return ErrLimit
			}
			if err := b.add(len(k) + 10); err != nil {
				return err
			}
			if err := b.walk(v.Field(f.index), depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrType
	}
}
