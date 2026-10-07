package descriptor

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// KeyLines maps the path of every key in raw to the 1-based line it opens
// on, a path joining object keys and list indexes with ".": "marker" is a
// top-level key, "2.severity" a key of the third entry of a top-level list
// and "check.0.id" one of the first [[check]] table. A document that does
// not parse maps nothing. TOML is read by its lines: tables, arrays of
// tables and bare or quoted keys, which is every shape a descriptor takes.
func KeyLines(raw []byte, f Format) map[string]int {
	out := map[string]int{}
	switch f {
	case JSON:
		jsonKeyLines(raw, out)
	case YAML:
		var n yaml.Node
		if yaml.Unmarshal(raw, &n) == nil && len(n.Content) > 0 {
			yamlKeyLines(n.Content[0], "", out)
		}
	case TOML:
		tomlKeyLines(raw, out)
	}
	return out
}

func join(prefix, seg string) string {
	if prefix == "" {
		return seg
	}
	return prefix + "." + seg
}

func jsonKeyLines(raw []byte, out map[string]int) {
	type frame struct {
		path    string
		array   bool
		index   int
		wantKey bool
		key     string
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var stack []*frame
	line := func() int { return bytes.Count(raw[:dec.InputOffset()], []byte("\n")) + 1 }
	// child is the path of the value about to be read in the top frame.
	child := func() string {
		if len(stack) == 0 {
			return ""
		}
		top := stack[len(stack)-1]
		if top.array {
			p := join(top.path, strconv.Itoa(top.index))
			top.index++
			return p
		}
		top.wantKey = true
		return join(top.path, top.key)
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		if len(stack) > 0 {
			top := stack[len(stack)-1]
			if !top.array && top.wantKey {
				if d, ok := tok.(json.Delim); ok && d == '}' {
					stack = stack[:len(stack)-1]
					continue
				}
				if k, ok := tok.(string); ok {
					top.key, top.wantKey = k, false
					out[join(top.path, k)] = line()
					continue
				}
			}
		}
		switch d := tok.(type) {
		case json.Delim:
			switch d {
			case '{':
				stack = append(stack, &frame{path: child(), wantKey: true})
			case '[':
				stack = append(stack, &frame{path: child(), array: true})
			case ']', '}':
				stack = stack[:len(stack)-1]
			}
		default:
			child()
		}
	}
}

func yamlKeyLines(n *yaml.Node, path string, out map[string]int) {
	if n.Kind == yaml.AliasNode {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			p := join(path, k.Value)
			out[p] = k.Line
			yamlKeyLines(n.Content[i+1], p, out)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			yamlKeyLines(c, join(path, strconv.Itoa(i)), out)
		}
	}
}

var (
	tomlArrayTable = regexp.MustCompile(`^\s*\[\[\s*([^\]]+?)\s*\]\]\s*(#.*)?$`)
	tomlTable      = regexp.MustCompile(`^\s*\[\s*([^\[\]]+?)\s*\]\s*(#.*)?$`)
	tomlKey        = regexp.MustCompile(`^\s*("(?:[^"\\]|\\.)*"|'[^']*'|[A-Za-z0-9_-]+)\s*=`)
)

func tomlKeyLines(raw []byte, out map[string]int) {
	table := ""
	counts := map[string]int{}
	for i, l := range strings.Split(string(raw), "\n") {
		if m := tomlArrayTable.FindStringSubmatch(l); m != nil {
			name := tomlName(m[1])
			table = join(name, strconv.Itoa(counts[name]))
			counts[name]++
			continue
		}
		if m := tomlTable.FindStringSubmatch(l); m != nil {
			table = tomlName(m[1])
			continue
		}
		if m := tomlKey.FindStringSubmatch(l); m != nil {
			k := m[1]
			if u, err := strconv.Unquote(k); err == nil && strings.HasPrefix(k, `"`) {
				k = u
			} else {
				k = strings.Trim(k, "'")
			}
			p := join(table, k)
			if _, seen := out[p]; !seen {
				out[p] = i + 1
			}
		}
	}
}

func tomlName(s string) string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if u, err := strconv.Unquote(p); err == nil && strings.HasPrefix(p, `"`) {
			p = u
		}
		parts[i] = strings.Trim(p, "'")
	}
	return strings.Join(parts, ".")
}
