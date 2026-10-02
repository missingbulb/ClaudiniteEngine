// Package jsjson reads JSON the way JavaScript's JSON.parse does and writes
// it back the way JSON.stringify does: an object keeps its key order (array
// indices first, ascending, then the rest as written; a repeated key keeps
// its first place and its last value), numbers print as Number#toString
// prints them and strings escape only what JSON.stringify escapes. A
// declaration's pattern over a serialized tool input then reads the same
// bytes on both engines.
package jsjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kind is a value's JavaScript type.
type Kind int

const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Value is one parsed JSON value.
type Value struct {
	Kind Kind
	Bool bool
	Num  float64
	Str  string
	Arr  []Value
	// Keys are an object's keys in JavaScript's order; Obj holds the values.
	Keys []string
	Obj  map[string]Value
}

// Decode parses one JSON document.
func Decode(raw []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decode(dec)
	if err != nil {
		return Value{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return Value{}, errors.New("jsjson: trailing data")
	}
	return v, nil
}

func decode(dec *json.Decoder) (Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return Value{}, err
	}
	switch t := tok.(type) {
	case nil:
		return Value{Kind: Null}, nil
	case bool:
		return Value{Kind: Bool, Bool: t}, nil
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return Value{}, err
		}
		return Value{Kind: Number, Num: f}, nil
	case string:
		return Value{Kind: String, Str: t}, nil
	case json.Delim:
		if t == '[' {
			v := Value{Kind: Array, Arr: []Value{}}
			for dec.More() {
				e, err := decode(dec)
				if err != nil {
					return Value{}, err
				}
				v.Arr = append(v.Arr, e)
			}
			_, err := dec.Token()
			return v, err
		}
		v := Value{Kind: Object, Obj: map[string]Value{}}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return Value{}, err
			}
			k, _ := kt.(string)
			e, err := decode(dec)
			if err != nil {
				return Value{}, err
			}
			if _, seen := v.Obj[k]; !seen {
				v.Keys = append(v.Keys, k)
			}
			v.Obj[k] = e
		}
		if _, err := dec.Token(); err != nil {
			return Value{}, err
		}
		v.Keys = jsOrder(v.Keys)
		return v, nil
	}
	return Value{}, fmt.Errorf("jsjson: unexpected token %v", tok)
}

// arrayIndex reports whether k is a canonical array index, which a
// JavaScript object lists before its other keys.
func arrayIndex(k string) (uint64, bool) {
	n, err := strconv.ParseUint(k, 10, 32)
	if err != nil || n == math.MaxUint32 || strconv.FormatUint(n, 10) != k {
		return 0, false
	}
	return n, true
}

func jsOrder(keys []string) []string {
	var idx, rest []string
	for _, k := range keys {
		if _, ok := arrayIndex(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.SliceStable(idx, func(i, j int) bool {
		a, _ := arrayIndex(idx[i])
		b, _ := arrayIndex(idx[j])
		return a < b
	})
	return append(idx, rest...)
}

// Truthy is JavaScript truthiness.
func (v Value) Truthy() bool {
	switch v.Kind {
	case Null:
		return false
	case Bool:
		return v.Bool
	case Number:
		return v.Num != 0 && !math.IsNaN(v.Num)
	case String:
		return v.Str != ""
	}
	return true
}

// Prop is v[key] for an array or object; ok is false for undefined.
func (v Value) Prop(key string) (Value, bool) {
	switch v.Kind {
	case Object:
		e, ok := v.Obj[key]
		return e, ok
	case Array:
		if key == "length" {
			return Value{Kind: Number, Num: float64(len(v.Arr))}, true
		}
		if n, ok := arrayIndex(key); ok && n < uint64(len(v.Arr)) {
			return v.Arr[n], true
		}
	}
	return Value{}, false
}

// FieldAt walks a dotted path as the Node engine's
// path.split('.').reduce((v, k) => v && typeof v === 'object' ? v[k] : undefined)
// does: a falsy value stops the walk and is the answer; ok is false for
// undefined.
func FieldAt(v Value, path string) (Value, bool) {
	cur, ok := v, true
	for _, key := range strings.Split(path, ".") {
		if !ok || !cur.Truthy() {
			continue
		}
		if cur.Kind == Object || cur.Kind == Array {
			cur, ok = cur.Prop(key)
			continue
		}
		cur, ok = Value{}, false
	}
	return cur, ok
}

// Stringify is JSON.stringify(v).
func Stringify(v Value) string {
	var b strings.Builder
	write(&b, v)
	return b.String()
}

func write(b *strings.Builder, v Value) {
	switch v.Kind {
	case Null:
		b.WriteString("null")
	case Bool:
		b.WriteString(strconv.FormatBool(v.Bool))
	case Number:
		if math.IsNaN(v.Num) || math.IsInf(v.Num, 0) {
			b.WriteString("null")
			return
		}
		b.WriteString(FormatNumber(v.Num))
	case String:
		Quote(b, v.Str)
	case Array:
		b.WriteByte('[')
		for i, e := range v.Arr {
			if i > 0 {
				b.WriteByte(',')
			}
			write(b, e)
		}
		b.WriteByte(']')
	case Object:
		b.WriteByte('{')
		for i, k := range v.Keys {
			if i > 0 {
				b.WriteByte(',')
			}
			Quote(b, k)
			b.WriteByte(':')
			write(b, v.Obj[k])
		}
		b.WriteByte('}')
	}
}

// Quote writes s as JSON.stringify quotes a string.
func Quote(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// FormatNumber is Number#toString for a finite number.
func FormatNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	a := math.Abs(f)
	if a >= 1e21 || a < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		mant, exp, _ := strings.Cut(s, "e")
		sign := "+"
		if exp[0] == '-' {
			sign = "-"
		}
		exp = strings.TrimLeft(exp[1:], "0")
		return mant + "e" + sign + exp
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Text is the text a pattern reads of a value, as the Node engine's
// typeof v === 'string' ? v : JSON.stringify(v) reads it.
func Text(v Value) string {
	if v.Kind == String {
		return v.Str
	}
	return Stringify(v)
}
