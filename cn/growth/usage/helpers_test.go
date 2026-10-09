package usage

import (
	"math"
	"sort"
	"strings"
	"testing"
)

// js is a JSON literal as the fold's value model holds it.
func js(t *testing.T, text string) any {
	t.Helper()
	v, err := ParseJSON(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return v
}

// jsObj is a JSON object literal.
func jsObj(t *testing.T, text string) *Obj {
	t.Helper()
	o, ok := js(t, text).(*Obj)
	if !ok {
		t.Fatalf("%s is not an object", text)
	}
	return o
}

// deepEqual is assert.deepStrictEqual over the value model: objects equal
// key-wise in any order, arrays element-wise.
func deepEqual(a, b any) bool {
	switch x := a.(type) {
	case *Obj:
		y, ok := b.(*Obj)
		if !ok || (x == nil) != (y == nil) {
			return false
		}
		if x == nil {
			return true
		}
		xk, yk := append([]string{}, x.Keys()...), append([]string{}, y.Keys()...)
		sort.Strings(xk)
		sort.Strings(yk)
		if strings.Join(xk, "\x00") != strings.Join(yk, "\x00") || len(xk) != len(yk) {
			return false
		}
		for _, k := range xk {
			xv, _ := x.Get(k)
			yv, _ := y.Get(k)
			if !deepEqual(xv, yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !deepEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.IsNaN(x) && math.IsNaN(y))
	}
	return a == b
}

// same fails unless got deep-equals the JSON literal want.
func same(t *testing.T, got any, want string) {
	t.Helper()
	if w := js(t, want); !deepEqual(got, w) {
		t.Fatalf("got  %s\nwant %s", Stringify(got), Stringify(w))
	}
}

// field is o's value at a dotted path, nil where any step is absent.
func field(o any, dotted string) any {
	v, _ := path(o, strings.Split(dotted, ".")...)
	return v
}

func fp(v float64) *float64 { return &v }
