package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/rewrite-temp/fromnode/node"
)

// A member's own packs may still hold what the Node engine read and cn
// refuses: a retired manifest field, and a declared check's severity. The
// move rewrites both, a file at a time, so the pack loads; a JavaScript
// rule it cannot port, and leftovers names it.

// ownPackEdit is one rewritten file of a member's own pack, and what
// changed in it.
type ownPackEdit struct {
	rel  string
	data []byte
	what []string
}

// ownPackEdits reads each declared local pack and returns the files the
// move rewrites. Only JSON is rewritten, the one spelling the Node engine
// read.
func ownPackEdits(repo string, local []string) ([]ownPackEdit, error) {
	var out []ownPackEdit
	for _, name := range local {
		rel := packset.LocalDir + "/" + name
		dir := filepath.Join(repo, filepath.FromSlash(rel))
		e, err := manifestEdit(dir, rel)
		if err != nil {
			return nil, err
		}
		if e != nil {
			out = append(out, *e)
		}
		files := []string{rel + "/declared-checks.json"}
		skills, _ := os.ReadDir(filepath.Join(dir, "skills"))
		for _, s := range skills {
			if s.IsDir() {
				files = append(files, rel+"/skills/"+s.Name()+"/declared-checks.json")
			}
		}
		for _, f := range files {
			e, err := severityEdit(repo, f)
			if err != nil {
				return nil, err
			}
			if e != nil {
				out = append(out, *e)
			}
		}
	}
	return out, nil
}

func manifestEdit(dir, rel string) (*ownPackEdit, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "pack.json"))
	if err != nil {
		return nil, nil
	}
	v, err := settings.DecodeOrdered(raw)
	if err != nil {
		return nil, fmt.Errorf("%s/pack.json: %w", rel, err)
	}
	obj, ok := v.(*settings.Ordered)
	if !ok {
		return nil, nil
	}
	var dropped []string
	for _, k := range node.RetiredManifestKeys {
		if _, ok := obj.Get(k); ok {
			obj.Delete(k)
			dropped = append(dropped, fmt.Sprintf("dropped %q, a Node manifest field cn does not read", k))
		}
	}
	if v, ok := obj.Get("version"); ok {
		if _, isString := v.(string); !isString {
			obj.Delete("version")
			dropped = append(dropped, fmt.Sprintf("dropped \"version\" %v: an own pack is not versioned, and cn reads a version only as a string", v))
		}
	}
	if len(dropped) == 0 {
		return nil, nil
	}
	return &ownPackEdit{rel: rel + "/pack.json", data: encodeOrdered(obj), what: dropped}, nil
}

func severityEdit(repo, rel string) (*ownPackEdit, error) {
	raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil
	}
	v, err := settings.DecodeOrdered(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	list, ok := v.([]any)
	if !ok {
		return nil, nil
	}
	var what []string
	for i, e := range list {
		decl, ok := e.(*settings.Ordered)
		if !ok {
			continue
		}
		sev, ok := decl.Get("severity")
		if !ok {
			continue
		}
		s, _ := sev.(string)
		to, known := node.RetiredOnFail[s]
		id, _ := decl.Get("id")
		if _, set := decl.Get("on_fail"); set {
			decl.Delete("severity")
			what = append(what, fmt.Sprintf("check %v: dropped severity %q beside its on_fail", id, s))
			continue
		}
		if !known {
			continue
		}
		renamed := settings.NewOrdered()
		for _, k := range decl.Keys() {
			val, _ := decl.Get(k)
			if k == "severity" {
				renamed.Set("on_fail", to)
				continue
			}
			renamed.Set(k, val)
		}
		list[i] = renamed
		what = append(what, fmt.Sprintf("check %v: severity %q became on_fail %q", id, s, to))
	}
	if len(what) == 0 {
		return nil, nil
	}
	return &ownPackEdit{rel: rel, data: encodeOrdered(list), what: what}, nil
}

// encodeOrdered writes v as two-space JSON in its own key order, with a
// trailing newline.
func encodeOrdered(v any) []byte {
	var b bytes.Buffer
	writeOrdered(&b, v, "")
	b.WriteString("\n")
	return b.Bytes()
}

func writeOrdered(b *bytes.Buffer, v any, indent string) {
	inner := indent + "  "
	switch x := v.(type) {
	case *settings.Ordered:
		if x.Len() == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range x.Keys() {
			val, _ := x.Get(k)
			key, _ := json.Marshal(k)
			b.WriteString(inner)
			b.Write(key)
			b.WriteString(": ")
			writeOrdered(b, val, inner)
			if i < x.Len()-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent + "}")
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range x {
			b.WriteString(inner)
			writeOrdered(b, e, inner)
			if i < len(x)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent + "]")
	default:
		var s bytes.Buffer
		enc := json.NewEncoder(&s)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(x)
		b.WriteString(strings.TrimSuffix(s.String(), "\n"))
	}
}

// applyOwnPackEdits writes the edits and returns a function that puts
// every file back as it was.
func applyOwnPackEdits(repo string, edits []ownPackEdit) (func(), error) {
	type saved struct {
		path string
		data []byte
	}
	var before []saved
	restore := func() {
		for _, s := range before {
			_ = os.WriteFile(s.path, s.data, 0o644)
		}
	}
	for _, e := range edits {
		path := filepath.Join(repo, filepath.FromSlash(e.rel))
		old, err := os.ReadFile(path)
		if err != nil {
			restore()
			return nil, err
		}
		before = append(before, saved{path, old})
		if err := os.WriteFile(path, e.data, 0o644); err != nil {
			restore()
			return nil, err
		}
	}
	return restore, nil
}

// jsRules lists the Node engine's coded rules under a pack directory,
// relative to it, sorted: worldRules/*.mjs, workRules/*.mjs and
// skills/*/checks.mjs. cn runs none of them.
func jsRules(dir string) []string {
	var out []string
	for _, scope := range []string{"worldRules", "workRules"} {
		entries, _ := os.ReadDir(filepath.Join(dir, scope))
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".mjs" {
				out = append(out, scope+"/"+e.Name())
			}
		}
	}
	skills, _ := os.ReadDir(filepath.Join(dir, "skills"))
	for _, s := range skills {
		if st, err := os.Stat(filepath.Join(dir, "skills", s.Name(), "checks.mjs")); s.IsDir() && err == nil && st.Mode().IsRegular() {
			out = append(out, "skills/"+s.Name()+"/checks.mjs")
		}
	}
	sort.Strings(out)
	return out
}
