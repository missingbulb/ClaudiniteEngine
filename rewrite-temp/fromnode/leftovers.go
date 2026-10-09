package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/rewrite-temp/fromnode/node"
)

// nodeHook matches a command that runs the Node engine's hooks, in a
// member (under .claudinite/shared/) or in the canon (from its root).
var nodeHook = regexp.MustCompile(`(^|[^\w.-])(\.claudinite/shared/)?engine/hooks/`)

// leftovers is what a moved member still carries of the Node engine: a
// hook command running it is a break, since that hook fails once the
// mount is gone, and so is a JavaScript rule in a local pack, which cn
// never runs; the rest are deprecations the move's pull request drops.
func leftovers(repo string) []findings.Finding {
	read := func(rel string) ([]byte, bool) {
		raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		return raw, err == nil
	}
	dep := func(path, sentence string) findings.Finding {
		return findings.Finding{Class: findings.Deprecation, ID: "node-leftovers", Path: path, Sentence: sentence}
	}
	var out []findings.Finding
	if event := nodeHookEvent(read); event != "" {
		out = append(out, findings.Finding{Class: findings.Break, ID: "hooks", Path: ".claude/settings.json",
			Sentence: event + " runs the Node engine's hooks (engine/hooks/), which the move removes; fromnode wires every hook to cn"})
	}
	if declared, err := packset.Declared(repo); err == nil {
		for _, name := range declared.Local {
			rel := packset.LocalDir + "/" + name
			rules := jsRules(filepath.Join(repo, filepath.FromSlash(rel)))
			for _, js := range rules {
				out = append(out, findings.Finding{Class: findings.Break, ID: "node-leftovers", Path: rel + "/" + js,
					Sentence: "a JavaScript check the Node engine ran, and cn runs none, so it no longer runs; port it to a declared check or a checks/*.go one, or delete it"})
			}
			broken := brokenImports(filepath.Join(repo, filepath.FromSlash(rel)), rules)
			names := make([]string, 0, len(broken))
			for f := range broken {
				names = append(names, f)
			}
			sort.Strings(names)
			for _, f := range names {
				out = append(out, findings.Finding{Class: findings.Break, ID: "node-leftovers", Path: rel + "/" + f,
					Sentence: "imports " + broken[f] + ", which is not there: the Node engine's mount it came from is gone; port it to @claudinite/sdk or the pack's own code"})
			}
		}
	}
	if _, _, err := settings.Find(repo); err == nil {
		if _, ok := read(node.File); ok {
			out = append(out, dep(node.File, "the Node engine's declaration, which cn no longer reads; the move pull request deletes it"))
		}
	}
	if st, err := os.Stat(filepath.Join(repo, ".claudinite", "shared", "engine")); err == nil && st.IsDir() {
		out = append(out, dep(".claudinite/shared/engine", "the Node engine's mount; the move removes .claudinite/shared/ and cn fetches the packs again"))
	}
	for _, name := range []string{"claudinite-rules.GENERATED.md", "claudinite-skills.GENERATED.md"} {
		rel := ".claudinite/" + name
		if _, ok := read(rel); ok {
			out = append(out, dep(rel, "an index at its path from before "+flatdecl.Dir+"/, which nothing writes any more; delete it"))
		}
	}
	workflows, _ := filepath.Glob(filepath.Join(repo, ".github", "workflows", "*.y*ml"))
	sort.Strings(workflows)
	for _, w := range workflows {
		rel := ".github/workflows/" + filepath.Base(w)
		if text, ok := read(rel); ok && strings.Contains(string(text), ".claudinite/shared/engine/checks/") {
			out = append(out, dep(rel, "a step runs the Node engine's checks under .claudinite/shared/engine/checks/, which the move removes; the move pull request drops the step (claudinite-ci.yml runs cn check world)"))
		}
	}
	if text, ok := read(".gitignore"); ok {
		hookLog, temp := false, false
		for _, l := range strings.Split(string(text), "\n") {
			l = strings.TrimSpace(l)
			if !hookLog && strings.HasPrefix(l, "/.claudinite-hooks.log") {
				hookLog = true
				out = append(out, dep(".gitignore", "ignores the Node engine's hook log ("+l+"), which cn never writes; the move pull request drops the line"))
			}
			if !temp && strings.TrimSuffix(strings.TrimPrefix(l, "/"), "/") == ".claudinite/temp" {
				temp = true
				out = append(out, dep(".gitignore", "ignores the session pack root ("+l+") from the repo root; .claudinite/.gitignore holds /temp/, so the move pull request drops the line and the comment above it"))
			}
		}
	}
	return out
}

// nodeHookEvent is the first event, by name, whose hooks run the Node
// engine's, or "".
func nodeHookEvent(read func(string) ([]byte, bool)) string {
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if raw, ok := read(".claude/settings.json"); ok {
		_ = json.Unmarshal(raw, &cfg)
	}
	events := make([]string, 0, len(cfg.Hooks))
	for e := range cfg.Hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, event := range events {
		for _, group := range cfg.Hooks[event] {
			for _, c := range group.Hooks {
				if nodeHook.MatchString(c.Command) {
					return event
				}
			}
		}
	}
	return ""
}
