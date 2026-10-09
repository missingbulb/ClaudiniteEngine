package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
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
// changed in it; remove deletes the file instead.
type ownPackEdit struct {
	rel    string
	data   []byte
	what   []string
	remove bool
}

// ownPackEdits reads each declared local pack and returns the files the
// move rewrites, removes or moves. A module manifest becomes pack.json;
// otherwise only JSON is rewritten, the one spelling the Node engine read.
func ownPackEdits(repo string, local []string) ([]ownPackEdit, error) {
	var out []ownPackEdit
	for _, name := range local {
		rel := packset.LocalDir + "/" + name
		dir := filepath.Join(repo, filepath.FromSlash(rel))
		module, err := moduleManifestEdits(dir, rel)
		if err != nil {
			return nil, err
		}
		out = append(out, module...)
		if module == nil {
			e, err := manifestEdit(dir, rel)
			if err != nil {
				return nil, err
			}
			if e != nil {
				out = append(out, *e)
			}
		}
		tasks, _ := filepath.Glob(filepath.Join(dir, "tasks", "*", "task.json"))
		sort.Strings(tasks)
		for _, t := range tasks {
			e, err := schemaEdit(repo, rel+"/tasks/"+filepath.Base(filepath.Dir(t))+"/task.json")
			if err != nil {
				return nil, err
			}
			if e != nil {
				out = append(out, *e)
			}
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
	dropped := retireManifestKeys(obj)
	if len(dropped) == 0 {
		return nil, nil
	}
	return &ownPackEdit{rel: rel + "/pack.json", data: encodeOrdered(obj), what: dropped}, nil
}

// retireManifestKeys drops what cn's manifest does not hold, and says
// what it dropped.
func retireManifestKeys(obj *settings.Ordered) []string {
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
	return dropped
}

// readModuleManifest is the Node script that imports a pack.mjs and
// prints its default export as JSON, functions left out, with each
// module it imports by a relative path whose default export a coded rule
// list holds, and how many listed rules none of them supplied.
const readModuleManifest = `
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
const url = pathToFileURL(process.argv[1]);
const manifest = (await import(url)).default ?? {};
const coded = [];
const listed = new Set();
for (const [, spec] of readFileSync(url, 'utf8').matchAll(/import\s+\w+\s+from\s+['"](\.{1,2}\/[^'"]+)['"]/g)) {
  const rule = (await import(new URL(spec, url))).default;
  for (const scope of ['worldRules', 'workRules']) {
    if ((manifest[scope] ?? []).includes(rule)) {
      coded.push({ scope, file: spec.replace(/^\.\//, '') });
      listed.add(rule);
    }
  }
}
const unmatched = ['worldRules', 'workRules'].flatMap((s) => manifest[s] ?? []).filter((r) => !listed.has(r)).length;
process.stdout.write(JSON.stringify({ manifest, coded, unmatched }, (k, v) => (typeof v === 'function' ? undefined : v)));
`

// moduleManifestEdits rewrites a local pack whose only manifest is the
// Node engine's pack.mjs: pack.json in its place, and each coded rule it
// listed moved under worldRules/ or workRules/, where leftovers names
// it. It reads pack.mjs with node, as the Node engine did; nil when the
// pack has a JSON manifest or no pack.mjs.
func moduleManifestEdits(dir, rel string) ([]ownPackEdit, error) {
	if _, err := os.Stat(filepath.Join(dir, "pack.json")); err == nil {
		return nil, nil
	}
	if st, err := os.Stat(filepath.Join(dir, packset.ModuleManifest)); err != nil || !st.Mode().IsRegular() {
		return nil, nil
	}
	cmd := exec.Command("node", "--input-type=module", "-e", readModuleManifest, filepath.Join(dir, packset.ModuleManifest))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s/%s: node could not read it (%v): %s", rel, packset.ModuleManifest, err, strings.TrimSpace(stderr.String()))
	}
	var read struct {
		Manifest  json.RawMessage `json:"manifest"`
		Coded     []struct{ Scope, File string }
		Unmatched int
	}
	if err := json.Unmarshal(raw, &read); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", rel, packset.ModuleManifest, err)
	}
	v, err := settings.DecodeOrdered(read.Manifest)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: %w", rel, packset.ModuleManifest, err)
	}
	obj, ok := v.(*settings.Ordered)
	if !ok {
		return nil, fmt.Errorf("%s/%s: its default export is not an object", rel, packset.ModuleManifest)
	}
	what := append([]string{"written from " + packset.ModuleManifest + ", the Node engine's module manifest, which cn does not read"}, retireManifestKeys(obj)...)
	if read.Unmatched > 0 {
		what = append(what, fmt.Sprintf("%d coded rule(s) %s listed came from no module it imports, so nothing names them for porting; read %s before it goes", read.Unmatched, packset.ModuleManifest, packset.ModuleManifest))
	}
	out := []ownPackEdit{
		{rel: rel + "/pack.json", data: encodeOrdered(obj), what: what},
		{rel: rel + "/" + packset.ModuleManifest, remove: true, what: []string{"removed: pack.json replaces it"}},
	}
	for _, c := range read.Coded {
		if strings.HasPrefix(c.File, c.Scope+"/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c.File)))
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", rel, c.File, err)
		}
		to := c.Scope + "/" + path.Base(c.File)
		out = append(out,
			ownPackEdit{rel: rel + "/" + to, data: data, what: []string{"moved from " + c.File + ", a coded rule " + packset.ModuleManifest + " listed in " + c.Scope}},
			ownPackEdit{rel: rel + "/" + c.File, remove: true, what: []string{"moved to " + to}})
	}
	return out, nil
}

// retiredSchema is the task schema the Node tasks pack shipped, which a
// task.json's "$schema" named and which the move removes.
const retiredSchema = "shared/packs/" + settings.RetiredTasksPack + "/"

func schemaEdit(repo, rel string) (*ownPackEdit, error) {
	raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil
	}
	v, err := settings.DecodeOrdered(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	obj, ok := v.(*settings.Ordered)
	if !ok {
		return nil, nil
	}
	schema, _ := obj.Get("$schema")
	if s, _ := schema.(string); !strings.Contains(s, retiredSchema) {
		return nil, nil
	}
	obj.Delete("$schema")
	return &ownPackEdit{rel: rel, data: encodeOrdered(obj), what: []string{`dropped "$schema", the Node tasks pack's schema the move removes`}}, nil
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
		path    string
		data    []byte
		created bool
	}
	var before []saved
	restore := func() {
		for i := len(before) - 1; i >= 0; i-- {
			s := before[i]
			if s.created {
				_ = os.Remove(s.path)
				continue
			}
			_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
			_ = os.WriteFile(s.path, s.data, 0o644)
		}
	}
	for _, e := range edits {
		path := filepath.Join(repo, filepath.FromSlash(e.rel))
		old, err := os.ReadFile(path)
		created := errors.Is(err, os.ErrNotExist) && !e.remove
		if err != nil && !created {
			restore()
			return nil, err
		}
		before = append(before, saved{path, old, created})
		if e.remove {
			err = os.Remove(path)
		} else if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = os.WriteFile(path, e.data, 0o644)
		}
		if err != nil {
			restore()
			return nil, err
		}
	}
	return restore, nil
}

// jsRules lists the Node engine's coded rules under a pack directory,
// relative to it, sorted: worldRules/*.mjs, workRules/*.mjs and
// skills/*/checks.mjs. cn runs none of them.
// relativeImport matches a module specifier a JavaScript file imports by
// a relative path.
var relativeImport = regexp.MustCompile(`(?m)(?:\bfrom|^\s*import|\bimport\s*\()\s*['"](\.{1,2}/[^'"]+)['"]`)

// brokenImports are the JavaScript files under dir, other than skip, that
// import a relative path that is not there: after the move, what they
// imported from the Node engine's mount.
func brokenImports(dir string, skip []string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || (filepath.Ext(p) != ".mjs" && filepath.Ext(p) != ".js") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if slices.Contains(skip, rel) {
			return nil
		}
		text, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range relativeImport.FindAllStringSubmatch(string(text), -1) {
			if _, err := os.Stat(filepath.Join(filepath.Dir(p), filepath.FromSlash(m[1]))); err != nil {
				out[rel] = m[1]
				break
			}
		}
		return nil
	})
	return out
}

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
