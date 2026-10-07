package addpacks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Obj is a JSON object that keeps a JavaScript object's key order:
// integer keys ascending, then every other key in insertion order. A
// force's config and answers travel into an issue body as the JSON a
// declaration will carry, so their order is what the operator typed.
type Obj struct {
	keys []string
	vals map[string]any
}

// NewObj is an empty object.
func NewObj() *Obj { return &Obj{vals: map[string]any{}} }

// Get is one key's value.
func (o *Obj) Get(k string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[k]
	return v, ok
}

// Set sets a key, keeping its place when it is already there.
func (o *Obj) Set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Len is the number of keys.
func (o *Obj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys are the keys in JavaScript's own order.
func (o *Obj) Keys() []string {
	if o == nil {
		return nil
	}
	var ints, rest []string
	for _, k := range o.keys {
		if arrayIndex(k) {
			ints = append(ints, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Slice(ints, func(i, j int) bool {
		a, _ := strconv.ParseUint(ints[i], 10, 32)
		b, _ := strconv.ParseUint(ints[j], 10, 32)
		return a < b
	})
	return append(ints, rest...)
}

func arrayIndex(k string) bool {
	n, err := strconv.ParseUint(k, 10, 32)
	return err == nil && n < 1<<32-1 && strconv.FormatUint(n, 10) == k
}

// MarshalJSON writes the object in its key order.
func (o *Obj) MarshalJSON() ([]byte, error) { return []byte(Stringify(o, "")), nil }

// Stringify is JavaScript's JSON.stringify(v, null, indent) for the values
// an add-packs body carries: Obj, maps (keys sorted), slices, strings,
// numbers, booleans and null.
func Stringify(v any, indent string) string {
	var b bytes.Buffer
	stringify(&b, v, indent, "")
	return b.String()
}

func stringify(b *bytes.Buffer, v any, indent, at string) {
	inner := at + indent
	open := func(c byte, n int, each func(i int)) {
		b.WriteByte(c)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteString("\n" + inner)
			}
			each(i)
		}
		if n > 0 && indent != "" {
			b.WriteString("\n" + at)
		}
		if c == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	}
	member := func(k string, val any) {
		b.WriteString(Quote(k))
		b.WriteByte(':')
		if indent != "" {
			b.WriteByte(' ')
		}
		stringify(b, val, indent, inner)
	}
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case *Obj:
		if t == nil {
			b.WriteString("null")
			return
		}
		keys := t.Keys()
		open('{', len(keys), func(i int) { member(keys[i], t.vals[keys[i]]) })
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		open('{', len(keys), func(i int) { member(keys[i], t[keys[i]]) })
	case []any:
		open('[', len(t), func(i int) { stringify(b, t[i], indent, inner) })
	case []string:
		open('[', len(t), func(i int) { b.WriteString(Quote(t[i])) })
	case string:
		b.WriteString(Quote(t))
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case float64:
		b.WriteString(jsNumber(t))
	case int:
		b.WriteString(strconv.Itoa(t))
	case json.Number:
		b.WriteString(t.String())
	default:
		raw, _ := json.Marshal(t)
		b.Write(raw)
	}
}

func jsNumber(f float64) string {
	if f == float64(int64(f)) && f < 1e21 && f > -1e21 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// Quote is JSON.stringify of one string: only the quote, the backslash
// and control characters are escaped.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// DecodeObj decodes JSON with every object as an *Obj in its source key
// order.
func DecodeObj(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObj()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(kt.(string), v)
			}
			_, err := dec.Token()
			return o, err
		case '[':
			out := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			_, err := dec.Token()
			return out, err
		}
	}
	return tok, nil
}
