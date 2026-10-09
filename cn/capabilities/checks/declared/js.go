package declared

import (
	"math"
	"strconv"
	"strings"
)

// The vocabulary was written against JavaScript values, and its families
// lean on how JavaScript reads them: an absent key is distinct from a
// null, a falsy value short-circuits a field walk, String(x) decides what
// a template prints. These helpers keep those readings exact.

type undefinedT struct{}

// undefined is JavaScript's undefined: a key that is not there.
var undefined = undefinedT{}

func isUndef(v any) bool {
	_, ok := v.(undefinedT)
	return ok
}

// truthy is JavaScript truthiness.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil, undefinedT:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case int:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// jsString is String(v).
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case undefinedT:
		return "undefined"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(x)
	case float64:
		return jsNumber(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e == nil || isUndef(e) {
				continue
			}
			parts[i] = jsString(e)
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	case *Regex:
		return "/" + x.Source + "/" + x.Flags
	}
	return ""
}

func jsNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	a := math.Abs(f)
	if a >= 1e21 || a < 1e-6 {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		mant, exp, _ := strings.Cut(s, "e")
		if exp[0] == '+' {
			exp = exp[1:]
		}
		exp = strings.TrimLeft(exp, "0")
		if strings.HasPrefix(exp, "-") {
			return mant + "e-" + strings.TrimLeft(exp[1:], "0")
		}
		return mant + "e+" + exp
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// strictEqual is ===: scalars by value, objects never (two parsed
// documents never share an object).
func strictEqual(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case undefinedT:
		return isUndef(b)
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	}
	return false
}

// prop is v[key] for a parsed value.
func prop(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		if e, ok := x[key]; ok {
			return e
		}
		return undefined
	case []any:
		if key == "length" {
			return float64(len(x))
		}
		if n, err := strconv.Atoi(key); err == nil && strconv.Itoa(n) == key && n >= 0 && n < len(x) {
			return x[n]
		}
		return undefined
	}
	return undefined
}

func isObject(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// fieldAt walks a dotted path as the Node engine's reduce does: a falsy
// value stops the walk and is the answer.
func fieldAt(doc any, path string) any {
	v := doc
	for _, key := range strings.Split(path, ".") {
		if truthy(v) && isObject(v) {
			v = prop(v, key)
			continue
		}
		if truthy(v) {
			v = undefined
		}
	}
	return v
}

func flatten(nodes []any) []any {
	var out []any
	for _, n := range nodes {
		if a, ok := n.([]any); ok {
			out = append(out, a...)
		} else {
			out = append(out, n)
		}
	}
	return out
}

// valuesAtPath collects the scalars a dotted path reaches, stepping
// through every array on the way.
func valuesAtPath(doc any, path string) []string {
	nodes := []any{doc}
	for _, key := range strings.Split(path, ".") {
		var next []any
		for _, n := range flatten(nodes) {
			var v any = undefined
			if truthy(n) && isObject(n) {
				v = prop(n, key)
			}
			if !isUndef(v) {
				next = append(next, v)
			}
		}
		nodes = next
	}
	var out []string
	for _, v := range flatten(nodes) {
		if v == nil || isObject(v) || isUndef(v) {
			continue
		}
		out = append(out, jsString(v))
	}
	return out
}

// namesAtField collects the non-empty strings at a dotted path, an absent
// last segment counting as defaultingTo when that is set.
func namesAtField(doc any, field string, defaultingTo any) []string {
	level := []any{doc}
	segs := strings.Split(field, ".")
	for i, seg := range segs {
		var next []any
		for _, node := range flatten(level) {
			m, ok := node.(map[string]any)
			if !ok {
				continue
			}
			v, has := m[seg]
			switch {
			case !has && i == len(segs)-1 && !isUndef(defaultingTo):
				next = append(next, defaultingTo)
			case has:
				next = append(next, v)
			}
		}
		level = next
	}
	var out []string
	for _, v := range flatten(level) {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// arrayHoldsValue reports whether values holds sought, by the matcher's
// case and entry-field rules.
func arrayHoldsValue(values any, matcher map[string]any, sought any) bool {
	arr, ok := values.([]any)
	if !ok {
		return false
	}
	norm := func(v any) string {
		if truthy(matcher["ignoreCase"]) {
			return strings.ToLower(jsString(v))
		}
		return jsString(v)
	}
	target := norm(sought)
	by, byField := matcher["matchingEntryObjectsByField"]
	for _, e := range arr {
		if byField && truthy(by) && truthy(e) && isObject(e) {
			if norm(fieldAt(e, jsString(by))) == target {
				return true
			}
			continue
		}
		if norm(e) == target {
			return true
		}
	}
	return false
}

// fill interpolates {name} from vars; an unknown name stays as written.
func fill(tpl any, vars map[string]any) string {
	s, ok := tpl.(string)
	if !ok {
		return jsString(tpl)
	}
	return replaceVars(s, func(key, whole string) string {
		if v, ok := vars[key]; ok {
			return jsString(v)
		}
		return whole
	})
}

func replaceVars(s string, f func(key, whole string) string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '{' {
			j := i + 1
			for j < len(s) && isWordByte(s[j]) {
				j++
			}
			if j > i+1 && j < len(s) && s[j] == '}' {
				b.WriteString(f(s[i+1:j], s[i:j+1]))
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// escapeRe escapes s for use inside a pattern.
func escapeRe(s string) string {
	var b strings.Builder
	for _, c := range s {
		if strings.ContainsRune(`.*+?^${}()|[]\`, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// globToRe is a whole-path glob: * any run, ? one character.
func globToRe(glob string) *Regex {
	var b strings.Builder
	b.WriteString("^")
	for _, c := range glob {
		switch {
		case c == '*':
			b.WriteString(".*")
		case c == '?':
			b.WriteString(".")
		case strings.ContainsRune(`.+^${}()|[]\`, c):
			b.WriteByte('\\')
			b.WriteRune(c)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteString("$")
	r, err := compileRegex(b.String(), "")
	if err != nil {
		return mustRegex(`(?!)`, "")
	}
	return r
}

// merge returns a new map holding a's entries then b's.
func merge(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
