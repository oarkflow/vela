// Package query implements field projections and equality predicates against
// borrowed VELA views or adaptive batches; it is not a SQL database.
package query

import (
	"bytes"
	"reflect"

	"github.com/oarkflow/vela"
	"github.com/oarkflow/vela/batch"
)

// Eq operates on the encoded value directly for common scalar types. It never
// materializes a map in these cases. Complex values use generic decoding.
func Eq(v vela.View, want any, path ...string) (bool, error) {
	f, found, err := v.Find(path...)
	if err != nil || !found {
		return false, err
	}
	switch x := want.(type) {
	case uint64:
		n, e := f.Uint()
		if e == vela.ErrType {
			return false, nil
		}
		return n == x, e
	case int64:
		n, e := f.Int()
		if e == vela.ErrType {
			return false, nil
		}
		return n == x, e
	case string:
		b, e := f.StringBytes()
		if e == vela.ErrType {
			return false, nil
		}
		return bytes.Equal(b, []byte(x)), e
	case bool:
		n, e := f.Bool()
		if e == vela.ErrType {
			return false, nil
		}
		return n == x, e
	case nil:
		b := f.Bytes()
		return len(b) == 1 && b[0] == vela.Null, nil
	}
	actual, err := f.Unmarshal()
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(actual, want), nil
}
func Project(v vela.View, paths ...string) (map[string]any, error) {
	result := make(map[string]any, len(paths))
	for _, path := range paths {
		f, found, err := v.Get(path)
		if err != nil {
			return nil, err
		}
		if found {
			val, e := f.Unmarshal()
			if e != nil {
				return nil, e
			}
			result[path] = val
		}
	}
	return result, nil
}
func BatchEq(b *batch.Batch, field string, want any) ([]int, error) { return b.FilterEq(field, want) }
