package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/verify"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

const launcherBody = "#!/bin/sh\n# the release's launcher\n"

// copyTree copies src into dst, keeping each file's mode; .git is left
// out.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// treeHash is every file under dir with its contents' hash, sorted.
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(p)
			s := sha256.Sum256(data)
			rel, _ := filepath.Rel(dir, p)
			lines = append(lines, filepath.ToSlash(rel)+" "+hex.EncodeToString(s[:8]))
		}
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// nodeHookCommands is every hook command in a .claude/settings.json that
// names an engine/hooks/ path, under any root.
func nodeHookCommands(t *testing.T, raw []byte) []string {
	t.Helper()
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf(".claude/settings.json: %v", err)
	}
	var out []string
	for _, groups := range cfg.Hooks {
		for _, g := range groups {
			for _, c := range g.Hooks {
				if strings.Contains(c.Command, "engine/hooks/") {
					out = append(out, c.Command)
				}
			}
		}
	}
	return out
}

func nodeMember(t *testing.T) string {
	repo := t.TempDir()
	copyTree(t, filepath.Join("testdata", "node-member"), repo)
	return repo
}

func TestMoveMovesANodeMember(t *testing.T) {
	repo := nodeMember(t)
	ci, _ := os.ReadFile(filepath.Join(repo, ".github/workflows/ci.yml"))
	in, out := input(t, repo)
	if err := move(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s := out.String()
	for _, want := range []string{"mapped packs", "pack: hello 1.0\n", "pack: base 2.0\n", "NEXT: git rm .claudinite-settings.json",
		"[ ] (cn) In the repository's Settings > Actions > General"} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "pack: local/mine") || strings.Contains(s, "seeded ") {
		t.Errorf("output:\n%s", s)
	}
	for _, gone := range []string{".claudinite/shared/engine", ".claudinite/shared/packs/hello/RULES.md.orig"} {
		if _, err := os.Stat(filepath.Join(repo, gone)); err == nil {
			t.Errorf("%s survived the move", gone)
		}
	}
	for _, kept := range []string{".claudinite-settings.json", ".claudinite/local/packs/mine/RULES.md", ".claudinite/local/packs/mine/pack.json", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(repo, kept)); err != nil {
			t.Errorf("the move removed the member's %s", kept)
		}
	}
	if h, _ := os.ReadFile(filepath.Join(repo, ".claudinite/shared/packs/hello/pack.json")); !strings.Contains(string(h), `"1.0"`) {
		t.Errorf("hello was not vendored again: %s", h)
	}
	if c, _ := os.ReadFile(filepath.Join(repo, ".github/workflows/ci.yml")); string(c) != string(ci) {
		t.Error("the move edited the member's ci.yml")
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	p, err := settings.ReadPacks(raw, settings.YAML)
	if err != nil || p.Channel != "canary" || strings.Join(p.Declared, ",") != "hello,base" || strings.Join(p.Local, ",") != "mine" {
		t.Errorf("packs %+v %v\n%s", p, err, raw)
	}
	cs, _ := os.ReadFile(filepath.Join(repo, ".claude/settings.json"))
	if hooks := nodeHookCommands(t, cs); len(hooks) > 0 {
		t.Errorf("the move kept the Node engine's hooks %v:\n%s", hooks, cs)
	}
	for _, want := range []string{"sh scripts/member-own-stop.sh", "sh scripts/member-own-lint.sh", "Bash(npm test)"} {
		if !strings.Contains(string(cs), want) {
			t.Errorf(".claude/settings.json lost %q:\n%s", want, cs)
		}
	}
	sched, _ := os.ReadFile(filepath.Join(repo, ".github/workflows/claudinite-scheduler.yml"))
	if !strings.Contains(string(sched), `    - cron: "26 4,16 * * *"`) {
		t.Errorf("the member's cron did not survive:\n%s", sched)
	}
	exec, _ := os.ReadFile(filepath.Join(repo, ".github/workflows/claudinite-executor.yml"))
	for _, s := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID", "CCR_ROUTINE_TOKEN"} {
		if !strings.Contains(string(exec), "          "+s+": ${{ secrets."+s+" }}\n") {
			t.Errorf("the executor lost %s:\n%s", s, exec)
		}
	}
	if g, _ := os.ReadFile(filepath.Join(repo, ".claudinite/.gitignore")); string(g) != "/temp/\nbin/\n" {
		t.Errorf(".claudinite/.gitignore %q", g)
	}
	// What verify says after the move: nothing about the hooks, the
	// workflows or the indexes; the leftovers are the Node engine's files
	// the move pull request drops.
	for _, f := range verify.Verify(verify.Input{Repo: repo, Launcher: []byte(launcherBody)}) {
		t.Errorf("verify: %s", f)
	}
	var paths []string
	for _, f := range leftovers(repo) {
		if f.ID != "node-leftovers" || f.Class != findings.Deprecation {
			t.Errorf("leftover: %s", f)
		}
		paths = append(paths, f.Path)
	}
	if strings.Join(paths, " ") != ".claudinite-settings.json .github/workflows/ci.yml .gitignore" {
		t.Errorf("node-leftovers %v", paths)
	}
}

// A refused import key, or a declared pack no index lists, leaves the repo
// as it was found.
func TestMoveRefusalWritesNothing(t *testing.T) {
	for name, decl := range map[string]string{
		"refused key":  `{"packs": ["hello"], "maintenance": {"automerge": true}}`,
		"unknown pack": `{"packs": ["hello", "nowhere"]}`,
		"not json":     `{"packs": [`,
	} {
		t.Run(name, func(t *testing.T) {
			repo := nodeMember(t)
			_ = os.WriteFile(filepath.Join(repo, ".claudinite-settings.json"), []byte(decl), 0o644)
			before := treeHash(t, repo)
			in, out := input(t, repo)
			if err := move(in); err == nil {
				t.Fatalf("moved:\n%s", out)
			}
			if after := treeHash(t, repo); after != before {
				t.Errorf("the refusal wrote:\n%s\nbefore:\n%s", after, before)
			}
		})
	}
}

func TestMoveRefusesWhatIsNotANodeMember(t *testing.T) {
	for name, edit := range map[string]func(string){
		"no declaration": func(r string) { _ = os.Remove(filepath.Join(r, ".claudinite-settings.json")) },
		"already pinned": func(r string) { _ = os.WriteFile(filepath.Join(r, ".claudinite/settings.toml"), nil, 0o644) },
		"launcher":       func(r string) { _ = os.WriteFile(filepath.Join(r, ".claudinite/launch"), nil, 0o644) },
	} {
		repo := nodeMember(t)
		edit(repo)
		in, out := input(t, repo)
		if err := move(in); err == nil {
			t.Errorf("%s: moved:\n%s", name, out)
		}
	}
}

// CLAUDINITE_PARITY_TREES names real Node members (colon-separated) and
// CLAUDINITE_PACKS_TREE a ClaudinitePacks checkout: each member is copied,
// moved with its canon packs published from that checkout, and verify
// then finds nothing; the leftovers hold no break.
func TestMoveOverRealMembers(t *testing.T) {
	trees, shelf := os.Getenv("CLAUDINITE_PARITY_TREES"), os.Getenv("CLAUDINITE_PACKS_TREE")
	if trees == "" || shelf == "" {
		t.Skip("CLAUDINITE_PARITY_TREES and CLAUDINITE_PACKS_TREE name no member trees and no packs checkout")
	}
	for _, src := range filepath.SplitList(trees) {
		if _, err := os.Stat(filepath.Join(src, ".claudinite-settings.json")); err != nil {
			continue
		}
		t.Run(filepath.Base(src), func(t *testing.T) {
			repo := t.TempDir()
			copyTree(t, src, repo)
			in, out := input(t, repo)
			in.Reader = shelfPacks(t, filepath.Join(shelf, "packs"))
			if err := move(in); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			t.Logf("fromnode over %s:\n%s", src, out)
			cs, _ := os.ReadFile(filepath.Join(repo, ".claude/settings.json"))
			if hooks := nodeHookCommands(t, cs); len(hooks) > 0 {
				t.Errorf("the move kept the Node engine's hooks %v", hooks)
			}
			var got []findings.Finding
			for _, f := range verify.Verify(verify.Input{Repo: repo, Launcher: []byte(launcherBody)}) {
				got = append(got, f)
				t.Errorf("verify: %s", f)
			}
			for _, f := range got {
				t.Logf("verify: %s", f)
			}
			for _, f := range leftovers(repo) {
				if f.Class == findings.Break {
					t.Errorf("leftover: %s", f)
				}
				t.Logf("leftover: %s", f)
			}
		})
	}
}

// shelfPacks publishes every pack folder under dir at its pack.json
// version, on the canary channel, with its requires.
func shelfPacks(t *testing.T, dir string) *fakePacks {
	t.Helper()
	f := &fakePacks{t: t, entries: map[string][]packindex.Entry{}, archives: map[string][]byte{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files := shelfFiles(t, filepath.Join(dir, e.Name()))
		if files == nil {
			continue
		}
		f.add(e.Name(), files)
	}
	return f
}

// shelfFiles is a pack folder's vendored set by tools/vendor's rule, nil
// for a folder with no pack.json.
func shelfFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "pack.json")); err != nil {
		return nil
	}
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if !strings.Contains(rel, "/") && (rel == "test" || rel == "docs" || rel == "provenance") {
				return filepath.SkipDir
			}
			return nil
		}
		if path.Dir(rel) == "checks" && strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(p)
		out[rel] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// add publishes files as pack id at its pack.json version and requires.
func (f *fakePacks) add(id string, files map[string][]byte) {
	var pj struct {
		Version  string   `json:"version"`
		Requires []string `json:"requires"`
	}
	if err := json.Unmarshal(files["pack.json"], &pj); err != nil {
		f.t.Fatalf("%s/pack.json: %v", id, err)
	}
	a := tgz(f.t, "", files)
	sum := sha256.Sum256(a)
	f.archives[id+"/"+pj.Version] = a
	f.entries[id] = append(f.entries[id], packindex.Entry{Version: pj.Version, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(a)),
		MinEngineVersion: ver, Channel: "canary", Requires: pj.Requires})
}

// A move that fails after it began writing removes the launcher and the
// settings file it wrote, so a re-run is not refused as a cn member, and
// names the restore for the rest.
func TestMoveFailurePartWayLeavesARerunnableRepo(t *testing.T) {
	repo := nodeMember(t)
	// The rules index cannot be written under a file.
	_ = os.RemoveAll(filepath.Join(repo, ".claudinite", "cache"))
	if err := os.WriteFile(filepath.Join(repo, ".claudinite", "cache"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	in, out := input(t, repo)
	err := move(in)
	if err == nil {
		t.Fatalf("moved:\n%s", out)
	}
	if !strings.Contains(err.Error(), "git checkout -- .") {
		t.Errorf("the error names no restore: %v", err)
	}
	for _, gone := range []string{".claudinite/launch", ".claudinite/settings.yaml"} {
		if _, err := os.Stat(filepath.Join(repo, gone)); err == nil {
			t.Errorf("%s survived the failed move", gone)
		}
	}
	if err := fromNodeState(repo); err != nil {
		t.Errorf("a re-run is refused: %v", err)
	}
}

// A Node scheduler with no cron line moves to the repo's own hashed cron
// rather than the template's placeholder; a member's own valid cron
// survives, in either quoting, a single daily tick and one the hash did
// not write included.
func TestMovedWorkflowKeepsTheMembersOwnCron(t *testing.T) {
	const n = "claudinite-scheduler.yml"
	forRepo := workflows.ForRepo("acme/widget")
	got, _ := movedWorkflow(n, []byte("on:\n  workflow_dispatch:\n"), "acme/widget")
	if string(got) != string(forRepo[n]) || strings.Contains(string(got), workflows.CronPlaceholder) {
		t.Errorf("no cron line: not the repo's template:\n%s", got)
	}
	for _, cron := range []string{"26 4,16 * * *", "39 4 * * *", "0 3 * * *", "26 4,17 * * *"} {
		for _, have := range []string{"    - cron: '" + cron + "'\n", "    - cron: \"" + cron + "\"\n"} {
			got, _ := movedWorkflow(n, []byte(have), "acme/widget")
			if !strings.Contains(string(got), `    - cron: "`+cron+`"`) || strings.Count(string(got), "- cron:") != 1 {
				t.Errorf("%q: the member's cron did not survive:\n%s", have, got)
			}
		}
	}
}

// MissingBulbWebsite: the move kept no owner-chosen single
// daily tick, rewriting 39 4 to the repo's hash.
func TestMoveKeepsAnOwnerChosenSingleTick(t *testing.T) {
	repo := nodeMember(t)
	p := filepath.Join(repo, ".github/workflows/claudinite-scheduler.yml")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), "'26 4,16 * * *'", "'39 4 * * *'", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	in, out := input(t, repo)
	if err := move(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	sched, _ := os.ReadFile(p)
	if !strings.Contains(string(sched), `    - cron: "39 4 * * *"`+"\n") {
		t.Errorf("the owner's single tick did not survive:\n%s", sched)
	}
}
