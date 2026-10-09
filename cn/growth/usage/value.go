// Package usage is the usage fold: the engine's built-in task that folds
// what a member's sessions did and what its own machinery cost into two
// rolling files under .claudinite/usage/, delivered on one pull request
// amended across runs.
//
// The session half reads the conversation-logs branch, the scheduler's and
// executor's run listings, the queue's closed items, the merged pull
// requests and the base branch's git history into
// .claudinite/usage/sessions-and-elements.json; the machinery half reads
// the run and jobs listings, the scheduler's job logs and the closed items'
// timelines into .claudinite/usage/task-runs-and-costs.json. Each half is
// fail-soft per source and per half: a source that cannot be read costs its
// own rows and leaves its own watermark, and a half that fails still lets
// the other land before the run fails naming it.
//
// Both files are written byte for byte as the Node task wrote them
// (missingbulb/ClaudinitePacks, packs/claudinite-tasks/tasks/usage-fold),
// which is why the fold works over Value, a JavaScript value with
// JavaScript's key order and arithmetic, rather than over typed rows: a
// prior file is decoded as it stands, junk included, and every counter
// folds the way the Node fold folded it.
package usage

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsregex"
)

// A Value is nil (null), bool, float64, string, []any or *Obj; an absent
// key is undefined.

// Obj is a JavaScript object: its keys in JavaScript's order, the
// canonical array indices first and ascending, the rest as inserted.
type Obj struct {
	keys []string
	vals map[string]any
}

// NewObj is {}.
func NewObj() *Obj { return &Obj{vals: map[string]any{}} }

// ObjOf is an object built from key-value pairs, in order.
func ObjOf(kv ...any) *Obj {
	o := NewObj()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

func arrayIndex(k string) (uint64, bool) {
	n, err := strconv.ParseUint(k, 10, 32)
	if err != nil || n == math.MaxUint32 || strconv.FormatUint(n, 10) != k {
		return 0, false
	}
	return n, true
}

// Get is o[k]; ok is false for undefined.
func (o *Obj) Get(k string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[k]
	return v, ok
}

// Has is `k in o`.
func (o *Obj) Has(k string) bool {
	_, ok := o.Get(k)
	return ok
}

// Set is o[k] = v.
func (o *Obj) Set(k string, v any) {
	if _, ok := o.vals[k]; ok {
		o.vals[k] = v
		return
	}
	o.vals[k] = v
	n, isIndex := arrayIndex(k)
	if !isIndex {
		o.keys = append(o.keys, k)
		return
	}
	at := len(o.keys)
	for i, e := range o.keys {
		m, ok := arrayIndex(e)
		if !ok || m > n {
			at = i
			break
		}
	}
	o.keys = append(o.keys, "")
	copy(o.keys[at+1:], o.keys[at:])
	o.keys[at] = k
}

// Delete is delete o[k].
func (o *Obj) Delete(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, e := range o.keys {
		if e == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Keys is Object.keys(o).
func (o *Obj) Keys() []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.keys...)
}

// Len is Object.keys(o).length.
func (o *Obj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Sub is (o[k] ??= {}) for a key that holds an object or nothing; a key
// holding anything else reads as a fresh object nothing keeps.
func (o *Obj) Sub(k string) *Obj {
	if v, ok := o.Get(k); ok && v != nil {
		if s, ok := v.(*Obj); ok {
			return s
		}
		return NewObj()
	}
	s := NewObj()
	o.Set(k, s)
	return s
}

// ObjAt is o[k] when it is an object, else nil.
func (o *Obj) ObjAt(k string) *Obj {
	v, _ := o.Get(k)
	s, _ := v.(*Obj)
	return s
}

// clone is structuredClone(v).
func clone(v any) any {
	switch x := v.(type) {
	case *Obj:
		if x == nil {
			return (*Obj)(nil)
		}
		out := &Obj{keys: append([]string(nil), x.keys...), vals: make(map[string]any, len(x.vals))}
		for k, e := range x.vals {
			out.vals[k] = clone(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = clone(e)
		}
		return out
	}
	return v
}

func cloneObj(o *Obj) *Obj {
	if o == nil {
		return NewObj()
	}
	return clone(o).(*Obj)
}

// spread is {...a, ...b}: b's own keys over a's.
func spread(objs ...*Obj) *Obj {
	out := NewObj()
	for _, o := range objs {
		for _, k := range o.Keys() {
			v, _ := o.Get(k)
			out.Set(k, v)
		}
	}
	return out
}

// fromJSON is a parsed JSON value as a Value.
func fromJSON(v jsjson.Value) any {
	switch v.Kind {
	case jsjson.Bool:
		return v.Bool
	case jsjson.Number:
		return v.Num
	case jsjson.String:
		return v.Str
	case jsjson.Array:
		out := make([]any, len(v.Arr))
		for i, e := range v.Arr {
			out[i] = fromJSON(e)
		}
		return out
	case jsjson.Object:
		o := NewObj()
		for _, k := range v.Keys {
			o.Set(k, fromJSON(v.Obj[k]))
		}
		return o
	}
	return nil
}

// ParseJSON is JSON.parse(text).
func ParseJSON(text string) (any, error) {
	v, err := jsjson.Decode([]byte(text))
	if err != nil {
		return nil, err
	}
	return fromJSON(v), nil
}

// Stringify is JSON.stringify(v) for a defined v.
func Stringify(v any) string {
	var b strings.Builder
	writeJSON(&b, v)
	return b.String()
}

func writeJSON(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null")
			return
		}
		b.WriteString(jsjson.FormatNumber(x))
	case string:
		jsjson.Quote(b, x)
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e)
		}
		b.WriteByte(']')
	case *Obj:
		if x == nil {
			b.WriteString("null")
			return
		}
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			jsjson.Quote(b, k)
			b.WriteByte(':')
			writeJSON(b, x.vals[k])
		}
		b.WriteByte('}')
	default:
		b.WriteString("null")
	}
}

// jsLess is a < b over two strings: UTF-16 code units, as JavaScript
// compares them.
func jsLess(a, b string) bool {
	if isBMPOnly(a) && isBMPOnly(b) {
		return lessBMP(a, b)
	}
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func isBMPOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xF0 {
			return false
		}
	}
	return true
}

func lessBMP(a, b string) bool { return a < b }

// jsLessThan is a < b over any two values, as JavaScript compares them:
// two strings by code units, anything else as numbers; defined is false
// where either side is NaN, which every comparison answers false.
func jsLessThan(a, b any) (less, defined bool) {
	pa, pb := toPrimitive(a), toPrimitive(b)
	sa, aText := pa.(string)
	sb, bText := pb.(string)
	if aText && bText {
		return jsLess(sa, sb), true
	}
	x, y := toNumber(pa), toNumber(pb)
	if math.IsNaN(x) || math.IsNaN(y) {
		return false, false
	}
	return x < y, true
}

// jsAtMost is a <= b.
func jsAtMost(a, b any) bool {
	greater, defined := jsLessThan(b, a)
	return defined && !greater
}

// jsSort is Array#sort() over strings, in place.
func jsSort(s []string) []string {
	sort.SliceStable(s, func(i, j int) bool { return jsLess(s[i], s[j]) })
	return s
}

// sortedKeys is Object.keys(o).sort().
func sortedKeys(o *Obj) []string { return jsSort(o.Keys()) }

// sortKeys is Object.fromEntries(Object.keys(o).sort().map(k => [k, o[k]])).
func sortKeys(o *Obj) *Obj {
	out := NewObj()
	for _, k := range sortedKeys(o) {
		v, _ := o.Get(k)
		out.Set(k, v)
	}
	return out
}

// truthy is JavaScript truthiness of a defined value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// toPrimitive is a value's primitive under the default hint: an array
// joins, an object is "[object Object]".
func toPrimitive(v any) any {
	switch x := v.(type) {
	case []any, *Obj:
		return jsString(x)
	}
	return v
}

// jsString is String(v).
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return jsjson.FormatNumber(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = jsString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// toNumber is Number(v).
func toNumber(v any) float64 {
	switch x := toPrimitive(v).(type) {
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case string:
		return stringToNumber(x)
	}
	return math.NaN()
}

func stringToNumber(s string) float64 {
	t := jsregex.Trim(s)
	if t == "" {
		return 0
	}
	if len(t) > 2 && t[0] == '0' && (t[1] == 'x' || t[1] == 'X' || t[1] == 'o' || t[1] == 'O' || t[1] == 'b' || t[1] == 'B') {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[t[1]]
		n, err := strconv.ParseUint(t[2:], base, 64)
		if err != nil {
			return math.NaN()
		}
		return float64(n)
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	for _, r := range t {
		if (r < '0' || r > '9') && r != '.' && r != 'e' && r != 'E' && r != '+' && r != '-' {
			return math.NaN()
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	return f
}

// plus is a + b.
func plus(a, b any) any {
	pa, pb := toPrimitive(a), toPrimitive(b)
	sa, aStr := pa.(string)
	sb, bStr := pb.(string)
	if aStr || bStr {
		if !aStr {
			sa = jsString(pa)
		}
		if !bStr {
			sb = jsString(pb)
		}
		return sa + sb
	}
	return toNumber(pa) + toNumber(pb)
}

// orZero is v ?? 0.
func orZero(v any, ok bool) any {
	if !ok || v == nil {
		return 0.0
	}
	return v
}

// jsMax is Math.max(a, b).
func jsMax(a, b any) float64 {
	x, y := toNumber(a), toNumber(b)
	switch {
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case x == 0 && y == 0:
		if math.Signbit(x) && math.Signbit(y) {
			return x
		}
		return 0
	case x > y:
		return x
	}
	return y
}

// jsMin is Math.min(a, b).
func jsMin(a, b any) float64 {
	x, y := toNumber(a), toNumber(b)
	switch {
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case x == 0 && y == 0:
		if math.Signbit(x) || math.Signbit(y) {
			return math.Copysign(0, -1)
		}
		return 0
	case x < y:
		return x
	}
	return y
}

// isFiniteNumber is typeof v === 'number' && Number.isFinite(v).
func isFiniteNumber(v any) bool {
	f, ok := v.(float64)
	return ok && !math.IsNaN(f) && !math.IsInf(f, 0)
}

// jsRound is Math.round(x): halves toward +Infinity.
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == math.Trunc(x) {
		return x
	}
	r := math.Floor(x)
	if x-r >= 0.5 {
		r++
	}
	if r == 0 && x < 0 {
		return math.Copysign(0, -1)
	}
	return r
}

// propOf is v?.[k] over any value: an object's own key, an array's
// index or length, undefined for anything else.
func propOf(v any, k string) (any, bool) {
	switch x := v.(type) {
	case *Obj:
		return x.Get(k)
	case []any:
		if k == "length" {
			return float64(len(x)), true
		}
		if n, ok := arrayIndex(k); ok && n < uint64(len(x)) {
			return x[n], true
		}
	case string:
		if k == "length" {
			return float64(len(utf16.Encode([]rune(x)))), true
		}
	}
	return nil, false
}

// path is v?.a?.b…, undefined where any step is.
func path(v any, keys ...string) (any, bool) {
	cur, ok := v, true
	for _, k := range keys {
		if !ok || cur == nil {
			return nil, false
		}
		cur, ok = propOf(cur, k)
	}
	return cur, ok
}

// pathString is v?.a?.b… when it is a string.
func pathString(v any, keys ...string) (string, bool) {
	x, _ := path(v, keys...)
	s, ok := x.(string)
	return s, ok
}

// nullish is `v ?? null`: the value, or nil where it is undefined.
func nullish(v any, ok bool) any {
	if !ok {
		return nil
	}
	return v
}
