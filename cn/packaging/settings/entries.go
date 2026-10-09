package settings

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/descriptor"
)

// The entry writer: an answer or a config key set on one declared pack,
// which turns a bare id into an entry object. A nested object cannot be
// edited by a line splice in all three formats, so the packs block is
// rewritten whole in its place: its keys keep their order, a comment
// inside it is lost, and every byte outside it stays as it was.

// SetAnswer records text verbatim as the answer to question on the entry
// token names.
func SetAnswer(raw []byte, f Format, token, question, text string) ([]byte, error) {
	if strings.TrimSpace(question) == "" {
		return nil, errors.New("an answer names its question")
	}
	return editEntry(raw, f, token, func(e *Ordered) error {
		answers, err := subObject(e, "answers", token)
		if err != nil {
			return err
		}
		answers.Set(question, text)
		return nil
	})
}

// SetEntryConfig sets config.<key> on the entry token names.
func SetEntryConfig(raw []byte, f Format, token, key string, value any) ([]byte, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("a config value names its key")
	}
	return editEntry(raw, f, token, func(e *Ordered) error {
		cfg, err := subObject(e, "config", token)
		if err != nil {
			return err
		}
		cfg.Set(key, value)
		return nil
	})
}

func subObject(e *Ordered, key, token string) (*Ordered, error) {
	v, ok := e.Get(key)
	if !ok || v == nil {
		o := NewOrdered()
		e.Set(key, o)
		return o, nil
	}
	o, ok := v.(*Ordered)
	if !ok {
		return nil, fmt.Errorf("the %s entry's %s is not an object", token, key)
	}
	return o, nil
}

// editEntry applies edit to the entry object token names, making one of a
// bare id, and rewrites the packs block.
func editEntry(raw []byte, f Format, token string, edit func(*Ordered) error) ([]byte, error) {
	packs, err := orderedPacks(raw, f)
	if err != nil {
		return nil, err
	}
	list, _ := packs.Get("declared")
	entries, _ := list.([]any)
	at := -1
	for i, e := range entries {
		switch x := e.(type) {
		case string:
			if x == token {
				at = i
			}
		case *Ordered:
			if id, _ := x.Get("id"); id == token {
				at = i
			}
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("packs.declared does not name %s", token)
	}
	entry, ok := entries[at].(*Ordered)
	if !ok {
		entry = NewOrdered()
		entry.Set("id", token)
		entries[at] = entry
	}
	if err := edit(entry); err != nil {
		return nil, err
	}
	packs.Set("declared", entries)
	return replacePacks(raw, f, packs)
}

// appendDeclared adds id to the end of packs.declared through the block
// rewrite, for a list a line edit cannot extend.
func appendDeclared(raw []byte, f Format, id string) ([]byte, error) {
	packs, err := orderedPacks(raw, f)
	if err != nil {
		return nil, err
	}
	list, _ := packs.Get("declared")
	entries, _ := list.([]any)
	packs.Set("declared", append(entries, id))
	return replacePacks(raw, f, packs)
}

// replacePacks writes packs in place of the file's packs block and reads
// the result back.
func replacePacks(raw []byte, f Format, packs *Ordered) ([]byte, error) {
	out, err := ReplaceBlock(raw, f, Block{Name: "packs", Value: packs})
	if err != nil {
		return nil, err
	}
	if _, err := ParseFile(out, f); err != nil {
		return nil, fmt.Errorf("the rewritten packs block does not read back: %w", err)
	}
	return out, nil
}

// orderedPacks is the file's packs block with its keys in file order.
func orderedPacks(raw []byte, f Format) (*Ordered, error) {
	if _, err := ParseFile(raw, f); err != nil {
		return nil, err
	}
	top, err := orderedTop(raw, f)
	if err != nil {
		return nil, err
	}
	v, ok := top.Get("packs")
	if !ok {
		return nil, fmt.Errorf("%s holds no packs block", RelPath(f))
	}
	o, ok := v.(*Ordered)
	if !ok {
		return nil, errors.New("packs is not an object")
	}
	return o, nil
}

// orderedTop parses the whole file with each object's keys in the order
// the file writes them: JSON by its decoder, YAML and TOML by the line
// each key opens on (keys sharing a line, an inline table's, are sorted).
func orderedTop(raw []byte, f Format) (*Ordered, error) {
	if f == JSON {
		v, err := DecodeOrdered(raw)
		if err != nil {
			return nil, err
		}
		o, ok := v.(*Ordered)
		if !ok {
			return nil, fmt.Errorf("%s must hold an object", RelPath(f))
		}
		return o, nil
	}
	v, err := descriptor.ParseBytes(raw, descriptor.Format(f))
	if err != nil {
		return nil, err
	}
	o, ok := withOrder(v, "", keyOrder{descriptor.KeyLines(raw, descriptor.Format(f)), strings.Split(string(raw), "\n")}).(*Ordered)
	if !ok {
		return nil, fmt.Errorf("%s must hold an object", RelPath(f))
	}
	return o, nil
}

// keyOrder places a key by the line it opens on, then by where on that
// line it is written.
type keyOrder struct {
	lines map[string]int
	text  []string
}

func (o keyOrder) less(path, a, b string) bool {
	la, lb := o.lines[path+a], o.lines[path+b]
	if la != lb && la > 0 && lb > 0 {
		return la < lb
	}
	from := la
	if from == 0 {
		from = o.ancestorLine(strings.TrimSuffix(path, "."))
	}
	pa, pb := o.position(from, a), o.position(from, b)
	if pa != pb {
		return pa < pb
	}
	return a < b
}

// ancestorLine is the line of the nearest enclosing key the parser
// placed: a key inside a TOML inline table has none of its own.
func (o keyOrder) ancestorLine(path string) int {
	for path != "" {
		if l, ok := o.lines[path]; ok {
			return l
		}
		i := strings.LastIndex(path, ".")
		if i < 0 {
			break
		}
		path = path[:i]
	}
	return 1
}

// position is where key is first written as a key at or after line, as
// line*width+column, or past every line.
func (o keyOrder) position(line int, key string) int {
	const width = 1 << 20
	re := regexp.MustCompile(`(^|[{,\s])"?` + regexp.QuoteMeta(key) + `"?\s*[=:]`)
	for i := max(line, 1) - 1; i < len(o.text); i++ {
		if loc := re.FindStringIndex(o.text[i]); loc != nil {
			return i*width + loc[0]
		}
	}
	return len(o.text) * width
}

func withOrder(v any, path string, order keyOrder) any {
	join := func(seg string) string {
		if path == "" {
			return seg
		}
		return path + "." + seg
	}
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		prefix := join("")
		sort.Slice(keys, func(i, k int) bool { return order.less(prefix, keys[i], keys[k]) })
		o := NewOrdered()
		for _, k := range keys {
			o.Set(k, withOrder(x[k], join(k), order))
		}
		return o
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withOrder(e, join(strconv.Itoa(i)), order)
		}
		return out
	}
	return v
}

var tomlHeader = regexp.MustCompile(`^[ \t]*\[\[?[ \t]*([^\[\]]+?)[ \t]*\]\]?[ \t]*(#.*)?$`)

// ReplaceBlock writes bl in place of the file's top-level block of its
// name, rendered as RenderBlocks renders it; every byte outside that
// block is kept. The block must already be there.
func ReplaceBlock(raw []byte, f Format, bl Block) ([]byte, error) {
	text, err := RenderBlocks(f, []Block{bl})
	if err != nil {
		return nil, err
	}
	start, end, err := blockRange(raw, f, bl.Name)
	if err != nil {
		return nil, err
	}
	if f == JSON {
		member := strings.TrimLeft(text, " ")
		value := member[strings.Index(member, ": ")+2:]
		return []byte(string(raw[:start]) + value + string(raw[end:])), nil
	}
	return []byte(string(raw[:start]) + text + string(raw[end:])), nil
}

// blockRange is the byte range of the named block: in YAML its header line
// through its last indented line, in TOML its tables (name and name.*)
// through the last non-blank line before the next table, in JSON the
// top-level member's value.
func blockRange(raw []byte, f Format, name string) (int, int, error) {
	lines := lineSpans(raw)
	text := func(s span) string { return strings.TrimSuffix(string(raw[s.start:s.end]), "\r") }
	blank := func(s span) bool { return strings.TrimSpace(text(s)) == "" }
	switch f {
	case YAML:
		header := patternsFor(name).yamlHeader
		first := -1
		for i, s := range lines {
			if header.MatchString(text(s)) {
				if first >= 0 {
					return 0, 0, fmt.Errorf("the settings hold the %s block twice", name)
				}
				first = i
			}
		}
		if first < 0 {
			return 0, 0, fmt.Errorf("%s holds no %s: block on a line of its own; edit it by hand", RelPath(f), name)
		}
		last := first
		for i := first + 1; i < len(lines) && !yamlEnd.MatchString(text(lines[i])); i++ {
			if !blank(lines[i]) {
				last = i
			}
		}
		return lines[first].start, lineEnd(raw, lines[last]), nil
	case TOML:
		first, last, cur, broke := -1, -1, "", false
		owned := func(t string) bool { return t == name || strings.HasPrefix(t, name+".") }
		for i, s := range lines {
			if m := tomlHeader.FindStringSubmatch(text(s)); m != nil {
				cur = strings.ReplaceAll(strings.Trim(m[1], " \t"), `"`, "")
				switch {
				case owned(cur) && broke:
					return 0, 0, fmt.Errorf("the [%s] tables are not together; edit them by hand", name)
				case owned(cur):
					if first < 0 {
						first = i
					}
					last = i
				case first >= 0:
					broke = true
				}
				continue
			}
			if first >= 0 && owned(cur) && !blank(s) {
				last = i
			}
		}
		if first < 0 {
			return 0, 0, fmt.Errorf("%s holds no [%s] table; edit it by hand", RelPath(f), name)
		}
		return lines[first].start, lineEnd(raw, lines[last]), nil
	case JSON:
		return jsonMemberValue(raw, name)
	}
	return 0, 0, fmt.Errorf("unknown settings format %q", f)
}

// jsonMemberValue finds the top-level member name's value.
func jsonMemberValue(raw []byte, name string) (int, int, error) {
	i := skipJSONSpace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return 0, 0, errors.New("the settings are not a JSON object")
	}
	i++
	for {
		i = skipJSONSpace(raw, i)
		if i < len(raw) && raw[i] == '}' {
			break
		}
		if i >= len(raw) || raw[i] != '"' {
			return 0, 0, errors.New("the settings object does not parse")
		}
		kEnd, err := jsonValueEnd(raw, i)
		if err != nil {
			return 0, 0, err
		}
		key, err := strconv.Unquote(string(raw[i:kEnd]))
		if err != nil {
			return 0, 0, err
		}
		i = skipJSONSpace(raw, kEnd)
		if i >= len(raw) || raw[i] != ':' {
			return 0, 0, errors.New("the settings object does not parse")
		}
		vStart := skipJSONSpace(raw, i+1)
		vEnd, err := jsonValueEnd(raw, vStart)
		if err != nil {
			return 0, 0, err
		}
		if key == name {
			return vStart, vEnd, nil
		}
		i = skipJSONSpace(raw, vEnd)
		if i < len(raw) && raw[i] == ',' {
			i++
		}
	}
	return 0, 0, fmt.Errorf("%s holds no %q member", RelPath(JSON), name)
}

func skipJSONSpace(raw []byte, i int) int {
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	return i
}

// jsonValueEnd is the offset just past the JSON value starting at i.
func jsonValueEnd(raw []byte, i int) (int, error) {
	if i >= len(raw) {
		return 0, errors.New("the settings end inside a value")
	}
	switch raw[i] {
	case '"':
		for j := i + 1; j < len(raw); j++ {
			switch raw[j] {
			case '\\':
				j++
			case '"':
				return j + 1, nil
			}
		}
		return 0, errors.New("the settings end inside a string")
	case '{', '[':
		depth := 0
		for j := i; j < len(raw); j++ {
			switch raw[j] {
			case '"':
				end, err := jsonValueEnd(raw, j)
				if err != nil {
					return 0, err
				}
				j = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1, nil
				}
			}
		}
		return 0, errors.New("the settings end inside an object")
	}
	j := i
	for j < len(raw) && !strings.ContainsRune(",}] \t\r\n", rune(raw[j])) {
		j++
	}
	return j, nil
}
