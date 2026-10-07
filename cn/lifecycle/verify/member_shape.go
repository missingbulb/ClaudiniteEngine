package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// ownPack is a member's own pack: a declared local pack or a temp pack
// present.
type ownPack struct {
	rel  string
	temp bool
}

func ownPacks(in Input) []ownPack {
	var out []ownPack
	for _, rel := range declaredTrees(in) {
		if strings.HasPrefix(rel, packset.LocalDir+"/") {
			out = append(out, ownPack{rel: rel})
		}
	}
	entries, _ := os.ReadDir(filepath.Join(in.Repo, filepath.FromSlash(packset.TempDir)))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, ownPack{rel: packset.TempDir + "/" + e.Name(), temp: true})
		}
	}
	return out
}

// checkLocalPackShape reads a member's own packs in the shapes the Node
// engine accepted: a retired manifest key or a declaration's severity is
// a deprecation, a JavaScript rule a break, since cn runs none.
func checkLocalPackShape(in Input) []findings.Finding {
	var out []findings.Finding
	for _, p := range ownPacks(in) {
		dir := filepath.Join(in.Repo, filepath.FromSlash(p.rel))
		where := "delete it"
		if p.temp {
			where = "delete it in the store repo's copy of this pack, which the session copies in"
		}
		if m, err := packset.ReadOwnManifest(dir); err == nil {
			raw, _ := os.ReadFile(filepath.Join(dir, m.File))
			for _, k := range m.Retired {
				f := dep("local-pack-shape", p.rel+"/"+m.File, fmt.Sprintf("declares %q, a retired manifest field nothing reads; %s", k, where))
				f.Line = descriptor.KeyLines(raw, descriptor.FormatOf(m.File))[k]
				out = append(out, f)
			}
		}
		for _, file := range declaredFiles(dir, p.rel) {
			out = append(out, severityUses(in, file)...)
		}
		for _, js := range packset.JSRules(dir) {
			id := strings.TrimSuffix(filepath.Base(js), ".mjs")
			if filepath.Base(js) == "checks.mjs" {
				id = filepath.Base(filepath.Dir(js))
			}
			out = append(out, brk("local-pack-shape", p.rel+"/"+js, fmt.Sprintf("is a JavaScript check, and cn runs none, so it no longer runs; port it to checks/%s.go or delete it", id)))
		}
	}
	return out
}

// declaredFiles are a pack's declared-checks files, its own and each
// skill's, relative to the repo.
func declaredFiles(dir, rel string) []string {
	var out []string
	add := func(d, r string) {
		if path, _, err := descriptor.Find(d, "declared-checks"); err == nil {
			out = append(out, r+"/"+filepath.Base(path))
		}
	}
	add(dir, rel)
	entries, _ := os.ReadDir(filepath.Join(dir, "skills"))
	for _, e := range entries {
		if e.IsDir() {
			add(filepath.Join(dir, "skills", e.Name()), rel+"/skills/"+e.Name())
		}
	}
	return out
}

func severityUses(in Input, rel string) []findings.Finding {
	raw, ok := read(in, rel)
	if !ok {
		return nil
	}
	v, err := descriptor.ParseBytes(raw, descriptor.FormatOf(rel))
	if err != nil {
		return nil
	}
	var list []any
	prefix := ""
	switch x := v.(type) {
	case []any:
		list = x
	case map[string]any:
		list, _ = x["check"].([]any)
		prefix = "check."
	}
	lines := descriptor.KeyLines(raw, descriptor.FormatOf(rel))
	var out []findings.Finding
	for i, e := range list {
		m, _ := e.(map[string]any)
		s, _ := m["severity"].(string)
		to, known := settings.RetiredOnFail[s]
		if !known {
			continue
		}
		id, _ := m["id"].(string)
		f := dep("local-pack-shape", rel, fmt.Sprintf("the declared check %q carries \"severity\": %q, the retired name of its on_fail; write \"on_fail\": %q", id, s, to))
		f.Line = lines[fmt.Sprintf("%s%d.severity", prefix, i)]
		out = append(out, f)
	}
	return out
}
