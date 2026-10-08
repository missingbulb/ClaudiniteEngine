// Package packseed reads a fleet manager's pack seeds, the declarations
// its fleet block asks every member to carry, and
// says when two configs are the same value. The pack-seed sweep writes
// what Parse returns, and the fleet-pack-seed-agrees check compares the
// same list against the manager's own entries, so the two never read a
// different list.
package packseed

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// Seed is one pack declaration the fleet wants in every member; Config is
// nil when the seed carries none.
type Seed struct {
	ID     string         `json:"id"`
	Config map[string]any `json:"config,omitempty"`
}

// Parse reads the packSeeds list of the entry's config, dropping an entry
// that is not an object or names no id, and keeping a config only when it
// is an object.
func Parse(cfg map[string]any) []Seed {
	list, _ := cfg["packSeeds"].([]any)
	var out []Seed
	for _, s := range list {
		o, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, ok := o["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			continue
		}
		seed := Seed{ID: strings.TrimSpace(id)}
		if c, ok := o["config"].(map[string]any); ok {
			seed.Config = c
		}
		out = append(out, seed)
	}
	return out
}

// Canonical is v as JSON with every object's keys sorted: two configs
// are the same value exactly when their canonical forms are equal.
func Canonical(v any) string {
	var b bytes.Buffer
	canonical(&b, v)
	return b.String()
}

func canonical(b *bytes.Buffer, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			canonical(b, t[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, x := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			canonical(b, x)
		}
		b.WriteByte(']')
	default:
		enc := json.NewEncoder(b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(t)
		b.Truncate(b.Len() - 1)
	}
}

// Agree reports whether two configs, either nil for none, are the same
// value.
func Agree(a, b any) bool { return Canonical(orNil(a)) == Canonical(orNil(b)) }

func orNil(v any) any {
	if m, ok := v.(map[string]any); ok && m == nil {
		return nil
	}
	return v
}
