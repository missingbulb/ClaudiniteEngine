// Package descriptor finds and parses the descriptors the engine reads by
// naming convention (pack, declared-checks, the member's settings): each
// may be written as .json, .yaml or .toml, exactly one per name in a
// folder, and all three parse into the same Go values (string, float64,
// bool, nil, []any, map[string]any) before one schema judges them. It is
// the one place the YAML and TOML parsers are imported.
package descriptor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	yaml "go.yaml.in/yaml/v3"
)

// Format is a descriptor's extension.
type Format string

const (
	JSON Format = "json"
	YAML Format = "yaml"
	TOML Format = "toml"
)

// Formats in the order a folder is searched; there is no priority, since
// two spellings of one name are refused.
var Formats = []Format{JSON, YAML, TOML}

var (
	// ErrAbsent is returned when a folder holds no spelling of the name.
	ErrAbsent = errors.New("absent")
	// ErrDuplicate is returned when a folder holds two or more spellings.
	ErrDuplicate = errors.New("more than one spelling")
)

// Find returns the one of dir/name.json, .yaml or .toml present.
func Find(dir, name string) (string, Format, error) {
	var found []string
	var formats []Format
	for _, f := range Formats {
		p := filepath.Join(dir, name+"."+string(f))
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			found = append(found, name+"."+string(f))
			formats = append(formats, f)
		}
	}
	switch len(found) {
	case 0:
		return "", "", fmt.Errorf("%s: %w", name, ErrAbsent)
	case 1:
		return filepath.Join(dir, found[0]), formats[0], nil
	}
	return "", "", fmt.Errorf("%s: %w: %s; keep one and delete the rest", name, ErrDuplicate, strings.Join(found, " and "))
}

// FormatOf is the format a file name's extension names, or "".
func FormatOf(path string) Format {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return JSON
	case ".yaml", ".yml":
		return YAML
	case ".toml":
		return TOML
	}
	return ""
}

// Parse finds dir/name in one format and parses it.
func Parse(dir, name string) (any, string, error) {
	path, f, err := Find(dir, name)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}
	v, err := ParseBytes(raw, f)
	if err != nil {
		return nil, path, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return v, path, nil
}

// ParseBytes parses raw in format f into the normalized values.
func ParseBytes(raw []byte, f Format) (any, error) {
	switch f {
	case JSON:
		if err := jsonDuplicateKey(raw); err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if dec.More() {
			return nil, errors.New("trailing data after the JSON value")
		}
		return v, nil
	case YAML:
		var n yaml.Node
		if err := yaml.Unmarshal(raw, &n); err != nil {
			return nil, err
		}
		return fromYAML(&n, false)
	case TOML:
		var v map[string]any
		if _, err := toml.Decode(string(raw), &v); err != nil {
			return nil, err
		}
		return normalize(v), nil
	}
	return nil, fmt.Errorf("unknown descriptor format %q", f)
}

// ParseDocument parses a repo document a check reads: as ParseBytes, but a
// YAML tag the parser does not know (a CloudFormation !Ref, !Sub, !GetAtt)
// is dropped and its value kept as if untagged, and a key a mapping names
// twice takes its last value, as a document's own readers resolve it.
func ParseDocument(raw []byte, f Format) (any, error) {
	if f != YAML {
		return ParseBytes(raw, f)
	}
	var n yaml.Node
	if err := yaml.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	return fromYAML(&n, true)
}

var standardTags = map[string]bool{"!!str": true, "!!int": true, "!!float": true, "!!bool": true, "!!null": true, "!!map": true, "!!seq": true, "!!binary": true, "!!timestamp": true, "!!merge": true}

func fromYAML(n *yaml.Node, dropTags bool) (any, error) {
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return fromYAML(n.Content[0], dropTags)
	case yaml.AliasNode:
		return fromYAML(n.Alias, dropTags)
	case yaml.SequenceNode:
		out := []any{}
		for _, c := range n.Content {
			v, err := fromYAML(c, dropTags)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		out := map[string]any{}
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Kind == yaml.ScalarNode && k.Tag == "!!merge" {
				merged, err := fromYAML(v, dropTags)
				if err != nil {
					return nil, err
				}
				if m, ok := merged.(map[string]any); ok {
					for mk, mv := range m {
						if _, set := out[mk]; !set {
							out[mk] = mv
						}
					}
				}
				continue
			}
			key, err := fromYAML(k, dropTags)
			if err != nil {
				return nil, err
			}
			val, err := fromYAML(v, dropTags)
			if err != nil {
				return nil, err
			}
			ks := keyString(key)
			if seen[ks] && !dropTags {
				return nil, fmt.Errorf("line %d: key %q appears twice in one mapping", k.Line, ks)
			}
			seen[ks] = true
			out[ks] = val
		}
		return out, nil
	case yaml.ScalarNode:
		c := *n
		if dropTags && c.Tag != "" && !standardTags[c.Tag] && c.Tag != "!" {
			c.Tag = ""
			if c.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
				c.Tag = "!!str"
			}
		}
		var v any
		if err := c.Decode(&v); err != nil {
			return nil, err
		}
		return normalize(v), nil
	}
	return nil, fmt.Errorf("unsupported YAML node kind %d", n.Kind)
}

// jsonDuplicateKey refuses an object naming one key twice, which
// encoding/json would resolve silently to the last.
func jsonDuplicateKey(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	type frame struct {
		object  bool
		keys    map[string]bool
		wantKey bool
	}
	var stack []*frame
	for {
		tok, err := dec.Token()
		if err != nil {
			// The decode that follows reports a syntax error in its own words.
			return nil
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				if top != nil && top.object {
					top.wantKey = true
				}
				stack = append(stack, &frame{object: t == '{', keys: map[string]bool{}, wantKey: true})
			case '}', ']':
				stack = stack[:len(stack)-1]
			}
		default:
			if top != nil && top.object {
				if top.wantKey {
					k, _ := t.(string)
					if top.keys[k] {
						return fmt.Errorf("key %q appears twice in one object", k)
					}
					top.keys[k] = true
					top.wantKey = false
				} else {
					top.wantKey = true
				}
			}
		}
	}
}

func keyString(k any) string {
	switch v := k.(type) {
	case string:
		return v
	case nil:
		return "null"
	case float64:
		return formatNumber(v)
	}
	return fmt.Sprint(k)
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprint(f)
}

// normalize maps the parsers' Go values onto the one shape encoding/json
// produces.
func normalize(v any) any {
	switch x := v.(type) {
	case nil, string, bool, float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case uint64:
		return float64(x)
	case float32:
		return float64(x)
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case []byte:
		return string(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = normalize(e)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[keyString(normalize(k))] = normalize(e)
		}
		return out
	}
	return fmt.Sprint(v)
}

// Kind is the type a schema key takes.
type Kind int

const (
	Any Kind = iota
	String
	Number
	Bool
	Object
	List
	StringList
	StringOrNull
	ObjectOrNull
)

func (k Kind) describe() string {
	switch k {
	case String:
		return "a string"
	case Number:
		return "a number"
	case Bool:
		return "true or false"
	case Object:
		return "an object"
	case List:
		return "a list"
	case StringList:
		return "a list of strings"
	case StringOrNull:
		return "a string or null"
	case ObjectOrNull:
		return "an object or null"
	}
	return "any value"
}

func (k Kind) accepts(v any) bool {
	switch k {
	case Any:
		return true
	case String:
		_, ok := v.(string)
		return ok
	case Number:
		_, ok := v.(float64)
		return ok
	case Bool:
		_, ok := v.(bool)
		return ok
	case Object:
		_, ok := v.(map[string]any)
		return ok
	case List:
		_, ok := v.([]any)
		return ok
	case StringList:
		l, ok := v.([]any)
		if !ok {
			return false
		}
		for _, e := range l {
			if _, ok := e.(string); !ok {
				return false
			}
		}
		return true
	case StringOrNull:
		_, ok := v.(string)
		return ok || v == nil
	case ObjectOrNull:
		_, ok := v.(map[string]any)
		return ok || v == nil
	}
	return false
}

// Schema is one descriptor's closed key vocabulary.
type Schema struct {
	Name     string
	Keys     map[string]Kind
	Required []string
}

// Validate judges an object against the schema: a required key missing, a
// key of the wrong type, or a key outside the vocabulary.
func (s Schema) Validate(obj map[string]any) []error {
	var errs []error
	for _, k := range s.Required {
		if _, ok := obj[k]; !ok {
			errs = append(errs, fmt.Errorf("%q is missing", k))
		}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		kind, ok := s.Keys[k]
		if !ok {
			errs = append(errs, fmt.Errorf("%q is not a %s key; the keys are %s", k, s.Name, strings.Join(s.names(), ", ")))
			continue
		}
		if !kind.accepts(obj[k]) {
			errs = append(errs, fmt.Errorf("%q must be %s", k, kind.describe()))
		}
	}
	return errs
}

func (s Schema) names() []string {
	out := make([]string, 0, len(s.Keys))
	for k := range s.Keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
