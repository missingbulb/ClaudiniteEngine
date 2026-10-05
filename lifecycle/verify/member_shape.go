package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings/node"
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

// checkNodeLeftovers deprecates what a half-moved member still carries
// of the Node engine.
func checkNodeLeftovers(in Input) []findings.Finding {
	var out []findings.Finding
	if _, _, err := settings.Find(in.Repo); err == nil {
		if _, ok := read(in, node.File); ok {
			out = append(out, dep("node-leftovers", node.File, "the Node engine's declaration, which cn no longer reads; the move pull request deletes it"))
		}
	}
	if st, err := os.Stat(filepath.Join(in.Repo, ".claudinite", "shared", "engine")); err == nil && st.IsDir() {
		out = append(out, dep("node-leftovers", ".claudinite/shared/engine", "the Node engine's mount; the move removes .claudinite/shared/ and cn fetches the packs again"))
	}
	for _, name := range []string{"claudinite-rules.GENERATED.md", "claudinite-skills.GENERATED.md"} {
		rel := ".claudinite/" + name
		if _, ok := read(in, rel); ok {
			out = append(out, dep("node-leftovers", rel, "an index at its path from before "+flatdecl.Dir+"/, which nothing writes any more; delete it"))
		}
	}
	workflows, _ := filepath.Glob(filepath.Join(in.Repo, ".github", "workflows", "*.y*ml"))
	sort.Strings(workflows)
	for _, w := range workflows {
		rel := ".github/workflows/" + filepath.Base(w)
		if text, ok := read(in, rel); ok && strings.Contains(string(text), ".claudinite/shared/engine/checks/") {
			out = append(out, dep("node-leftovers", rel, "a step runs the Node engine's checks under .claudinite/shared/engine/checks/, which the move removes; the move pull request drops the step (claudinite-ci.yml runs cn check world)"))
		}
	}
	if text, ok := read(in, ".gitignore"); ok {
		hookLog, temp := false, false
		for _, l := range strings.Split(string(text), "\n") {
			l = strings.TrimSpace(l)
			if !hookLog && strings.HasPrefix(l, "/.claudinite-hooks.log") {
				hookLog = true
				out = append(out, dep("node-leftovers", ".gitignore", "ignores the Node engine's hook log ("+l+"), which cn never writes; the move pull request drops the line"))
			}
			if !temp && strings.TrimSuffix(strings.TrimPrefix(l, "/"), "/") == ".claudinite/temp" {
				temp = true
				out = append(out, dep("node-leftovers", ".gitignore", "ignores the session pack root ("+l+") from the repo root; .claudinite/.gitignore holds /temp/, so the move pull request drops the line and the comment above it"))
			}
		}
	}
	return out
}
