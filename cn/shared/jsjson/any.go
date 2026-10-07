package jsjson

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// FromAny is v, a value encoding/json or shared/descriptor decoded
// (string, float64, json.Number, bool, nil, []any, map[string]any), as a
// Value. A map has no order of its own, so its keys come out in
// JavaScript's order over their sorted spelling.
func FromAny(v any) Value {
	switch x := v.(type) {
	case nil:
		return Value{Kind: Null}
	case bool:
		return Value{Kind: Bool, Bool: x}
	case float64:
		return Value{Kind: Number, Num: x}
	case int:
		return Value{Kind: Number, Num: float64(x)}
	case json.Number:
		f, _ := x.Float64()
		return Value{Kind: Number, Num: f}
	case string:
		return Value{Kind: String, Str: x}
	case []any:
		out := Value{Kind: Array, Arr: make([]Value, len(x))}
		for i, e := range x {
			out.Arr[i] = FromAny(e)
		}
		return out
	case []string:
		out := Value{Kind: Array, Arr: make([]Value, len(x))}
		for i, e := range x {
			out.Arr[i] = Value{Kind: String, Str: e}
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := Value{Kind: Object, Keys: jsOrder(keys), Obj: map[string]Value{}}
		for k, e := range x {
			out.Obj[k] = FromAny(e)
		}
		return out
	}
	return Value{Kind: Null}
}

// StringifyAny is JSON.stringify of a decoded value.
func StringifyAny(v any) string { return Stringify(FromAny(v)) }

// StringOf is JavaScript's String(v) of a decoded value: an array joins
// its elements' strings with commas (null among them as the empty
// string), an object is "[object Object]".
func StringOf(v any) string {
	return stringOf(FromAny(v))
}

func stringOf(v Value) string {
	switch v.Kind {
	case Null:
		return "null"
	case Bool:
		return strconv.FormatBool(v.Bool)
	case Number:
		return FormatNumber(v.Num)
	case String:
		return v.Str
	case Array:
		parts := make([]string, len(v.Arr))
		for i, e := range v.Arr {
			if e.Kind != Null {
				parts[i] = stringOf(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// StringOfValue is JavaScript's String(v) of a parsed value.
func StringOfValue(v Value) string { return stringOf(v) }
