package vela

import (
	"reflect"
	"strings"
	"sync"
)

type structField struct {
	index int
	name  string
	omit  bool
}

var structFields sync.Map // map[reflect.Type][]structField

// vela tags take precedence over json tags. Both accept name, omitempty and -.
func fieldsFor(v reflect.Value, omitEmpty bool) []structField {
	t := v.Type()
	if cached, ok := structFields.Load(t); ok {
		return selectFields(v, cached.([]structField), omitEmpty)
	}
	fields := make([]structField, 0, t.NumField())
	seen := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, ok := f.Tag.Lookup("vela")
		if !ok {
			tag = f.Tag.Get("json")
		}
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			continue
		}
		name := parts[0]
		if name == "" {
			name = f.Name
		}
		omit := false
		for _, option := range parts[1:] {
			if option == "omitempty" {
				omit = true
			}
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		fields = append(fields, structField{index: i, name: name, omit: omit})
	}
	actual, _ := structFields.LoadOrStore(t, fields)
	return selectFields(v, actual.([]structField), omitEmpty)
}

func selectFields(v reflect.Value, fields []structField, omitEmpty bool) []structField {
	if !omitEmpty {
		return fields
	}
	for i, field := range fields {
		if field.omit && v.Field(field.index).IsZero() {
			selected := make([]structField, 0, len(fields)-1)
			selected = append(selected, fields[:i]...)
			for _, next := range fields[i+1:] {
				if !next.omit || !v.Field(next.index).IsZero() {
					selected = append(selected, next)
				}
			}
			return selected
		}
	}
	return fields
}
