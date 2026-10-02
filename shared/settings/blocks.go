package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
)

// A whole top-level block written into a settings file that already pins
// the engine: the packs and checks blocks an import produces. The file is
// edited by splicing the rendered blocks in, never by re-serializing it,
// so the engine block and every comment survive.

// Ordered is a JSON-shaped object that keeps its keys in the order they
// were set. A block's values are string, json.Number, bool, nil, []any and
// *Ordered.
type Ordered struct {
	keys []string
	vals map[string]any
}

// NewOrdered is an empty object.
func NewOrdered() *Ordered { return &Ordered{vals: map[string]any{}} }

// Set sets k, keeping its place when it is already there.
func (o *Ordered) Set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Get returns k's value.
func (o *Ordered) Get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// Delete removes k.
func (o *Ordered) Delete(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Keys are the keys in order.
func (o *Ordered) Keys() []string { return append([]string{}, o.keys...) }

// Len is the number of keys.
func (o *Ordered) Len() int { return len(o.keys) }

// Plain is the value with every *Ordered turned into a map, as the
// descriptor parsers return it.
func Plain(v any) any {
	switch x := v.(type) {
	case *Ordered:
		m := map[string]any{}
		for _, k := range x.keys {
			m[k] = Plain(x.vals[k])
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Plain(e)
		}
		return out
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return x.String()
		}
		return f
	}
	return v
}

// DecodeOrdered parses one JSON value, its objects as *Ordered and its
// numbers as json.Number. A key named twice keeps its first place and its
// last value, as JSON.parse reads it.
func DecodeOrdered(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("trailing data after the JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch d := t.(type) {
	case json.Delim:
		switch d {
		case '{':
			o := NewOrdered()
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
		return nil, fmt.Errorf("unexpected %v", d)
	}
	return t, nil
}

// Block is one top-level block to write.
type Block struct {
	Name  string
	Value *Ordered
}

// RenderBlocks renders blocks as the text they take in a file of format
// f, each block whole.
func RenderBlocks(f Format, blocks []Block) (string, error) {
	var b strings.Builder
	for _, bl := range blocks {
		var s string
		var err error
		switch f {
		case YAML:
			s, err = yamlBlock(bl)
		case TOML:
			s, err = tomlBlock(bl)
		case JSON:
			s, err = jsonMember(bl, 1)
		default:
			err = fmt.Errorf("unknown settings format %q", f)
		}
		if err != nil {
			return "", err
		}
		if f == JSON && b.Len() > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// SpliceBlocks adds blocks to a settings file that holds the engine pin
// and none of them: appended at the end of a YAML or TOML file, and as the
// last members of a JSON file's object. No byte already there changes.
func SpliceBlocks(raw []byte, f Format, blocks []Block) ([]byte, error) {
	if _, err := ReadEngine(raw, f); err != nil {
		return nil, err
	}
	v, err := ParseBytesTop(raw, f)
	if err != nil {
		return nil, err
	}
	for _, bl := range blocks {
		if _, ok := v[bl.Name]; ok {
			return nil, fmt.Errorf("%s already holds a %s block", RelPath(f), bl.Name)
		}
	}
	text, err := RenderBlocks(f, blocks)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return raw, nil
	}
	switch f {
	case YAML, TOML:
		out := append([]byte{}, raw...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		if f == TOML {
			out = append(out, '\n')
		}
		return append(out, text...), nil
	}
	end := bytes.LastIndexByte(raw, '}')
	if end < 0 {
		return nil, fmt.Errorf("%s holds no object to add to", RelPath(f))
	}
	body := bytes.TrimRight(raw[:end], " \t\r\n")
	out := append([]byte{}, body...)
	if len(body) > 0 && body[len(body)-1] != '{' {
		out = append(out, ',')
	}
	out = append(out, '\n')
	out = append(out, text...)
	out = append(out, '\n')
	return append(out, raw[end:]...), nil
}

// ParseBytesTop parses the settings file into its top-level object.
func ParseBytesTop(raw []byte, f Format) (map[string]any, error) {
	v, err := descriptor.ParseBytes(raw, descriptor.Format(f))
	if err != nil {
		return nil, fmt.Errorf("%s does not parse: %w", RelPath(f), err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must hold an object", RelPath(f))
	}
	return obj, nil
}

// quote is a string as JSON writes it, which YAML's double-quoted scalar
// and TOML's basic string both read the same.
func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

var (
	plainYAMLKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	yamlReserved = map[string]bool{"true": true, "false": true, "null": true, "yes": true, "no": true, "on": true, "off": true, "y": true, "n": true, "~": true}
	bareTOMLKey  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

func yamlKey(k string) string {
	if plainYAMLKey.MatchString(k) && !yamlReserved[strings.ToLower(k)] {
		return k
	}
	return quote(k)
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "null", true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case string:
		return quote(x), true
	case json.Number:
		return x.String(), true
	case float64:
		return json.Number(fmt.Sprint(x)).String(), true
	case []any:
		if len(x) == 0 {
			return "[]", true
		}
	case *Ordered:
		if x.Len() == 0 {
			return "{}", true
		}
	}
	return "", false
}

func yamlBlock(bl Block) (string, error) {
	lines, err := yamlLines(bl.Value, 2)
	if err != nil {
		return "", err
	}
	return bl.Name + ":\n" + strings.Join(lines, "\n") + "\n", nil
}

// yamlLines renders a collection's lines at indent spaces.
func yamlLines(v any, indent int) ([]string, error) {
	pad := strings.Repeat(" ", indent)
	var out []string
	switch x := v.(type) {
	case *Ordered:
		for _, k := range x.keys {
			val := x.vals[k]
			if s, ok := scalar(val); ok {
				out = append(out, pad+yamlKey(k)+": "+s)
				continue
			}
			sub, err := yamlLines(val, indent+2)
			if err != nil {
				return nil, err
			}
			out = append(out, pad+yamlKey(k)+":")
			out = append(out, sub...)
		}
	case []any:
		for _, e := range x {
			if s, ok := scalar(e); ok {
				out = append(out, pad+"- "+s)
				continue
			}
			sub, err := yamlLines(e, indent+2)
			if err != nil {
				return nil, err
			}
			sub[0] = pad + "- " + sub[0][indent+2:]
			out = append(out, sub...)
		}
	default:
		return nil, fmt.Errorf("a %T is not a YAML collection", v)
	}
	return out, nil
}

func tomlKey(k string) string {
	if bareTOMLKey.MatchString(k) {
		return k
	}
	return quote(k)
}

// tomlInline renders v on one line.
func tomlInline(v any, at string) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", fmt.Errorf("%s is null, which TOML cannot hold; write the settings as YAML or JSON", at)
	case *Ordered:
		var parts []string
		for _, k := range x.keys {
			s, err := tomlInline(x.vals[k], at+"."+k)
			if err != nil {
				return "", err
			}
			parts = append(parts, tomlKey(k)+" = "+s)
		}
		if len(parts) == 0 {
			return "{}", nil
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case []any:
		var parts []string
		for i, e := range x {
			s, err := tomlInline(e, fmt.Sprintf("%s[%d]", at, i))
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}
	s, _ := scalar(v)
	return s, nil
}

// tomlBlock renders a block as a table: scalars and lists as key = value
// lines (a list one element per line), a nested object as a subtable.
func tomlBlock(bl Block) (string, error) {
	var b strings.Builder
	if err := tomlTable(&b, bl.Name, bl.Value); err != nil {
		return "", err
	}
	return b.String(), nil
}

func tomlTable(b *strings.Builder, header string, o *Ordered) error {
	fmt.Fprintf(b, "[%s]\n", header)
	var subs []string
	for _, k := range o.keys {
		v := o.vals[k]
		if sub, ok := v.(*Ordered); ok && sub.Len() > 0 {
			subs = append(subs, k)
			continue
		}
		if list, ok := v.([]any); ok && len(list) > 0 {
			fmt.Fprintf(b, "%s = [\n", tomlKey(k))
			for i, e := range list {
				s, err := tomlInline(e, fmt.Sprintf("%s.%s[%d]", header, k, i))
				if err != nil {
					return err
				}
				fmt.Fprintf(b, "  %s,\n", s)
			}
			b.WriteString("]\n")
			continue
		}
		s, err := tomlInline(v, header+"."+k)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "%s = %s\n", tomlKey(k), s)
	}
	for _, k := range subs {
		b.WriteString("\n")
		if err := tomlTable(b, header+"."+tomlKey(k), o.vals[k].(*Ordered)); err != nil {
			return err
		}
	}
	return nil
}

// jsonMember renders "name": value at depth, two spaces a level.
func jsonMember(bl Block, depth int) (string, error) {
	var b strings.Builder
	b.WriteString(strings.Repeat("  ", depth) + quote(bl.Name) + ": ")
	jsonValue(&b, bl.Value, depth)
	return b.String(), nil
}

func jsonValue(b *strings.Builder, v any, depth int) {
	if s, ok := scalar(v); ok {
		b.WriteString(s)
		return
	}
	pad := strings.Repeat("  ", depth+1)
	switch x := v.(type) {
	case *Ordered:
		b.WriteString("{\n")
		for i, k := range x.keys {
			b.WriteString(pad + quote(k) + ": ")
			jsonValue(b, x.vals[k], depth+1)
			if i < len(x.keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(strings.Repeat("  ", depth) + "}")
	case []any:
		b.WriteString("[\n")
		for i, e := range x {
			b.WriteString(pad)
			jsonValue(b, e, depth+1)
			if i < len(x)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(strings.Repeat("  ", depth) + "]")
	}
}
