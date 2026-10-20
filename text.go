package vela

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MarshalText encodes a value in VELA text v1. It is intended for readable
// HTTP bodies; the compact binary wire format remains the default.
func MarshalText(value any) ([]byte, error) { return MarshalTextWithLimits(value, Limits{}) }

func MarshalTextWithLimits(value any, limits Limits) ([]byte, error) {
	limits = limits.normalized()
	budget := &inputBudget{limits: limits}
	if err := budget.walk(reflect.ValueOf(value), 0); err != nil {
		return nil, err
	}
	out, err := appendText(nil, value)
	if err != nil {
		return nil, err
	}
	if len(out) > limits.MaxBytes {
		return nil, ErrLimit
	}
	return out, nil
}

func appendText(dst []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case time.Time:
		return appendJSONString(append(dst, 't'), x.Format(time.RFC3339Nano))
	case time.Duration:
		return appendJSONString(append(dst, 'd'), x.String())
	case nil:
		return append(dst, "null"...), nil
	case bool:
		if x {
			return append(dst, "true"...), nil
		}
		return append(dst, "false"...), nil
	case uint64:
		return strconv.AppendUint(append(dst, 'u'), x, 10), nil
	case int64:
		return strconv.AppendInt(append(dst, 'i'), x, 10), nil
	case float32:
		dst = append(dst, "f32("...)
		dst = strconv.AppendFloat(dst, float64(x), 'g', -1, 32)
		return append(dst, ')'), nil
	case float64:
		dst = append(dst, 'f')
		dst = strconv.AppendFloat(dst, x, 'g', -1, 64)
		return dst, nil
	case string:
		return appendJSONString(dst, x)
	case []byte:
		dst = append(dst, "b64\""...)
		dst = base64.StdEncoding.AppendEncode(dst, x)
		return append(dst, '"'), nil
	case []any:
		dst = append(dst, '[')
		for i, element := range x {
			if i > 0 {
				dst = append(dst, ';')
			}
			var err error
			dst, err = appendText(dst, element)
			if err != nil {
				return nil, err
			}
		}
		return append(dst, ']'), nil
	case map[string]any:
		dst = append(dst, '{')
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i > 0 {
				dst = append(dst, ';')
			}
			if isTextIdentifier(key) {
				dst = append(dst, key...)
			} else {
				var err error
				dst, err = appendJSONString(dst, key)
				if err != nil {
					return nil, err
				}
			}
			dst = append(dst, '=')
			var err error
			dst, err = appendText(dst, x[key])
			if err != nil {
				return nil, err
			}
		}
		return append(dst, '}'), nil
	default:
		return appendTextReflect(dst, reflect.ValueOf(v))
	}
}

func appendTextReflect(dst []byte, v reflect.Value) ([]byte, error) {
	if !v.IsValid() {
		return append(dst, "null"...), nil
	}
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return append(dst, "null"...), nil
		}
		if v.Kind() == reflect.Pointer && v.Elem().Type() == reflect.TypeOf(time.Time{}) {
			v = v.Elem()
			continue
		}
		if v.CanInterface() {
			if marshaler, ok := v.Interface().(encoding.TextMarshaler); ok {
				b, err := marshaler.MarshalText()
				if err != nil {
					return nil, err
				}
				return appendText(dst, string(b))
			}
		}
		v = v.Elem()
	}
	if v.CanInterface() {
		if _, ok := v.Interface().(time.Time); ok {
			return appendText(dst, v.Interface())
		}
		if _, ok := v.Interface().(time.Duration); ok {
			return appendText(dst, v.Interface())
		}
		if marshaler, ok := v.Interface().(encoding.TextMarshaler); ok {
			b, err := marshaler.MarshalText()
			if err != nil {
				return nil, err
			}
			return appendText(dst, string(b))
		}
	}
	switch v.Kind() {
	case reflect.Bool:
		return appendText(dst, v.Bool())
	case reflect.String:
		return appendText(dst, v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendText(dst, v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return appendText(dst, v.Uint())
	case reflect.Float32:
		return appendText(dst, float32(v.Float()))
	case reflect.Float64:
		return appendText(dst, v.Float())
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return append(dst, "null"...), nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, v.Len())
			for i := range b {
				b[i] = byte(v.Index(i).Uint())
			}
			return appendText(dst, b)
		}
		dst = append(dst, '[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				dst = append(dst, ';')
			}
			var err error
			dst, err = appendTextReflect(dst, v.Index(i))
			if err != nil {
				return nil, err
			}
		}
		return append(dst, ']'), nil
	case reflect.Map:
		if v.IsNil() {
			return append(dst, "null"...), nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return nil, ErrType
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		dst = append(dst, '{')
		for i, key := range keys {
			if i > 0 {
				dst = append(dst, ';')
			}
			var err error
			dst, err = appendTextKey(dst, key.String())
			if err != nil {
				return nil, err
			}
			dst = append(dst, '=')
			dst, err = appendTextReflect(dst, v.MapIndex(key))
			if err != nil {
				return nil, err
			}
		}
		return append(dst, '}'), nil
	case reflect.Struct:
		fields := append([]structField(nil), fieldsFor(v, true)...)
		sort.Slice(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
		dst = append(dst, '{')
		for i, field := range fields {
			if i > 0 {
				dst = append(dst, ';')
			}
			var err error
			dst, err = appendTextKey(dst, field.name)
			if err != nil {
				return nil, err
			}
			dst = append(dst, '=')
			dst, err = appendTextReflect(dst, v.Field(field.index))
			if err != nil {
				return nil, err
			}
		}
		return append(dst, '}'), nil
	default:
		return nil, fmt.Errorf("vela: unsupported text value %s: %w", v.Type(), ErrType)
	}
}

func appendTextKey(dst []byte, key string) ([]byte, error) {
	if isTextIdentifier(key) {
		return append(dst, key...), nil
	}
	return appendJSONString(dst, key)
}

func appendJSONString(dst []byte, s string) ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return append(dst, b...), nil
}

func isTextIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// UnmarshalText decodes a complete VELA text v1 value.
func UnmarshalText(data []byte) (any, error) { return UnmarshalTextWithLimits(data, Limits{}) }

func UnmarshalTextWithLimits(data []byte, limits Limits) (any, error) {
	limits = limits.normalized()
	if len(data) > limits.MaxBytes {
		return nil, ErrLimit
	}
	if !utf8.Valid(data) {
		return nil, ErrInvalid
	}
	p := textParser{data: data, limits: limits}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos != len(data) {
		return nil, ErrInvalid
	}
	return v, nil
}

// UnmarshalTextInto decodes VELA text into a non-nil typed pointer.
func UnmarshalTextInto(data []byte, dst any, limits Limits) error {
	out := reflect.ValueOf(dst)
	if !out.IsValid() || out.Kind() != reflect.Pointer || out.IsNil() {
		return ErrType
	}
	v, err := UnmarshalTextWithLimits(data, limits)
	if err != nil {
		return err
	}
	tmp := reflect.New(out.Elem().Type()).Elem()
	if err := assignValue(tmp, v); err != nil {
		return err
	}
	out.Elem().Set(tmp)
	return nil
}

type textParser struct {
	data   []byte
	pos    int
	nodes  int
	limits Limits
}

func (p *textParser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

func (p *textParser) take(c byte) bool {
	p.space()
	if p.pos < len(p.data) && p.data[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

func (p *textParser) word(s string) bool {
	p.space()
	if bytes.HasPrefix(p.data[p.pos:], []byte(s)) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *textParser) quoted() (string, error) {
	p.space()
	if p.pos >= len(p.data) || p.data[p.pos] != '"' {
		return "", ErrInvalid
	}
	start := p.pos
	p.pos++
	escaped := false
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		p.pos++
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			var s string
			if err := json.Unmarshal(p.data[start:p.pos], &s); err != nil {
				return "", ErrInvalid
			}
			return s, nil
		}
	}
	return "", ErrInvalid
}

func (p *textParser) identifier() (string, error) {
	p.space()
	start := p.pos
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (p.pos > start && c >= '0' && c <= '9') {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", ErrInvalid
	}
	return string(p.data[start:p.pos]), nil
}

func (p *textParser) integer(signed bool) (any, error) {
	start := p.pos
	if signed && p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
	digits := p.pos
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == digits {
		return nil, ErrInvalid
	}
	if signed {
		n, err := strconv.ParseInt(string(p.data[start:p.pos]), 10, 64)
		if err != nil {
			return nil, ErrInvalid
		}
		return n, nil
	}
	n, err := strconv.ParseUint(string(p.data[start:p.pos]), 10, 64)
	if err != nil {
		return nil, ErrInvalid
	}
	return n, nil
}

func (p *textParser) floating(bits int) (any, error) {
	if !p.take('(') {
		return nil, ErrInvalid
	}
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] != ')' {
		p.pos++
	}
	if p.pos == len(p.data) {
		return nil, ErrInvalid
	}
	text := strings.TrimSpace(string(p.data[start:p.pos]))
	p.pos++
	if !validFloatLiteral(text) {
		return nil, ErrInvalid
	}
	n, err := strconv.ParseFloat(text, bits)
	if err != nil {
		return nil, ErrInvalid
	}
	if bits == 32 {
		return float32(n), nil
	}
	return n, nil
}

func (p *textParser) bareFloat() (float64, error) {
	start := p.pos
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ';' || c == ']' || c == '}' || c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			break
		}
		p.pos++
	}
	s := string(p.data[start:p.pos])
	if !validFloatLiteral(s) {
		return 0, ErrInvalid
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return n, nil
}

func validFloatLiteral(s string) bool {
	if s == "NaN" || s == "+Inf" || s == "-Inf" {
		return true
	}
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(s)
}

func (p *textParser) value(depth int) (any, error) {
	if depth > p.limits.MaxDepth {
		return nil, ErrLimit
	}
	p.nodes++
	if p.nodes > p.limits.MaxNodes {
		return nil, ErrLimit
	}
	p.space()
	if p.pos >= len(p.data) {
		return nil, ErrInvalid
	}
	switch p.data[p.pos] {
	case '"':
		s, err := p.quoted()
		if err != nil {
			return nil, err
		}
		if len(s) > p.limits.MaxStringBytes {
			return nil, ErrLimit
		}
		return s, nil
	case '{':
		return p.mapValue(depth)
	case 'u':
		p.pos++
		return p.integer(false)
	case 'i':
		p.pos++
		return p.integer(true)
	case 's':
		p.pos++
		s, err := p.quoted()
		if err != nil {
			return nil, err
		}
		if len(s) > p.limits.MaxStringBytes {
			return nil, ErrLimit
		}
		return s, nil
	case 'b':
		if !p.word("b64") {
			return nil, ErrInvalid
		}
		s, err := p.quoted()
		if err != nil {
			return nil, err
		}
		if uint64(len(s)) > ((uint64(p.limits.MaxStringBytes)+2)/3)*4 {
			return nil, ErrLimit
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, ErrInvalid
		}
		if len(b) > p.limits.MaxStringBytes {
			return nil, ErrLimit
		}
		return b, nil
	case 'f':
		if p.word("false") {
			return false, nil
		}
		if p.word("f32") {
			return p.floating(32)
		}
		if p.word("f64") {
			return p.floating(64)
		}
		p.pos++
		return p.bareFloat()
	case 't':
		if p.word("true") {
			return true, nil
		}
		p.pos++
		s, err := p.quoted()
		if err != nil {
			return nil, err
		}
		if len(s) > p.limits.MaxStringBytes {
			return nil, ErrLimit
		}
		value, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, ErrInvalid
		}
		return value, nil
	case 'd':
		p.pos++
		s, err := p.quoted()
		if err != nil {
			return nil, err
		}
		if len(s) > p.limits.MaxStringBytes {
			return nil, ErrLimit
		}
		value, err := time.ParseDuration(s)
		if err != nil {
			return nil, ErrInvalid
		}
		return value, nil
	case 'n':
		if p.word("null") {
			return nil, nil
		}
	case 'm':
		if p.word("map") {
			return p.mapValue(depth)
		}
	case '[':
		return p.arrayValue(depth)
	}
	return nil, ErrInvalid
}

func (p *textParser) mapValue(depth int) (any, error) {
	if !p.take('{') {
		return nil, ErrInvalid
	}
	out := make(map[string]any)
	if p.take('}') {
		return out, nil
	}
	for {
		if len(out) >= p.limits.MaxContainerEntries {
			return nil, ErrLimit
		}
		p.space()
		var key string
		var err error
		if p.pos < len(p.data) && p.data[p.pos] == '"' {
			key, err = p.quoted()
		} else {
			key, err = p.identifier()
		}
		if err != nil {
			return nil, err
		}
		if len(key) > p.limits.MaxKeyBytes {
			return nil, ErrLimit
		}
		if _, exists := out[key]; exists {
			return nil, ErrInvalid
		}
		if !p.take('=') {
			return nil, ErrInvalid
		}
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out[key] = v
		if p.take('}') {
			return out, nil
		}
		if !p.take(';') {
			return nil, ErrInvalid
		}
	}
}

func (p *textParser) arrayValue(depth int) (any, error) {
	if !p.take('[') {
		return nil, ErrInvalid
	}
	out := make([]any, 0)
	if p.take(']') {
		return out, nil
	}
	for {
		if len(out) >= p.limits.MaxContainerEntries {
			return nil, ErrLimit
		}
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if p.take(']') {
			return out, nil
		}
		if !p.take(';') {
			return nil, ErrInvalid
		}
	}
}
