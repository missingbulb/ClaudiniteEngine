package parity

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// yamlDoc renders a JSON-shaped value as block YAML: maps and lists in
// block style with sorted keys, every string double-quoted with JSON's
// escapes (a subset of YAML's double-quoted scalar), so no plain scalar
// can be read as a bool, a number or a null.
func yamlDoc(v any) (string, error) {
	var b strings.Builder
	if err := yamlBlock(&b, v, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

func yamlBlock(b *strings.Builder, v any, indent int) error {
	pad := strings.Repeat("  ", indent)
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			key, _ := json.Marshal(k)
			if s, ok := yamlScalar(x[k]); ok {
				fmt.Fprintf(b, "%s%s: %s\n", pad, key, s)
				continue
			}
			fmt.Fprintf(b, "%s%s:\n", pad, key)
			if err := yamlBlock(b, x[k], indent+1); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range x {
			if s, ok := yamlScalar(e); ok {
				fmt.Fprintf(b, "%s- %s\n", pad, s)
				continue
			}
			// A nested block opens on its own line under the dash.
			fmt.Fprintf(b, "%s-\n", pad)
			if err := yamlBlock(b, e, indent+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("a %T at the top of a YAML block", v)
	}
	return nil
}

// yamlScalar renders v inline when it is a scalar or an empty collection.
func yamlScalar(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "null", true
	case bool, float64, int:
		return fmt.Sprint(x), true
	case string:
		s, _ := json.Marshal(x)
		return string(s), true
	case map[string]any:
		if len(x) == 0 {
			return "{}", true
		}
	case []any:
		if len(x) == 0 {
			return "[]", true
		}
	}
	return "", false
}
