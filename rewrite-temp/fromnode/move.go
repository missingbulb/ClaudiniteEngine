package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/adopt"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/rewrite-temp/fromnode/node"
)

// The move's mechanical half: a Node member becomes a cn member. The
// declaration is read into the pinned settings, Claudinite's regenerated
// tree is vendored again from published versions, the Node hooks give way
// to cn's and the workflows are written through Expected, so a member's
// stamped secrets and cron survive. No member-owned file is deleted: the
// Node declaration stays until the move's pull request drops it.

// localTree answers the Node import's question of a member's own packs.
type localTree string

// HasLocal reports whether .claudinite/local/packs/<name>/ is a directory.
func (t localTree) HasLocal(name string) bool {
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return false
	}
	st, err := os.Stat(filepath.Join(string(t), filepath.FromSlash(packset.LocalDir), name))
	return err == nil && st.IsDir()
}

// fromNodeState refuses a repo that is not a Node member awaiting its
// move, naming what it found.
func fromNodeState(repo string) error {
	var found []string
	for _, f := range settings.Formats {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(settings.RelPath(f)))); err == nil {
			found = append(found, settings.RelPath(f))
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".claudinite", "launch")); err == nil {
		found = append(found, ".claudinite/launch")
	}
	if len(found) > 0 {
		return fmt.Errorf("%s already exists: this repo is a cn member, and fromnode moves a Node member only", strings.Join(found, " and "))
	}
	if _, err := os.Stat(filepath.Join(repo, node.File)); err != nil {
		return fmt.Errorf("%s is absent: fromnode moves a Node member, which declares its packs there; cn init --packs adopts from nothing", node.File)
	}
	return nil
}

// move moves in.Repo from the Node engine to cn. Every read happens
// first: the declaration's import, the engine and every declared canon
// pack; a refused import key or an unresolvable pack writes nothing.
func move(in adopt.Input) error {
	if err := fromNodeState(in.Repo); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(in.Repo, node.File))
	if err != nil {
		return err
	}
	decl, rep, err := node.Read(raw, localTree(in.Repo))
	if err != nil {
		return err
	}
	fmt.Fprint(in.Out, rep.String())
	if rep.Refused() {
		return fmt.Errorf("the import refused a key of %s; the repo is as it was", node.File)
	}
	got, err := adopt.PickEngine(&in)
	if err != nil {
		return err
	}
	cfg, err := movedSettings(in.Channel, got.Version, got.Integrity, decl)
	if err != nil {
		return err
	}
	declared, err := settings.ReadPacks(cfg, settings.YAML)
	if err != nil {
		return fmt.Errorf("the imported settings do not read back: %w", err)
	}
	edits, err := ownPackEdits(in.Repo, declared.Local)
	if err != nil {
		return err
	}
	// The queue is the engine's: a pack still requiring the retired tasks
	// pack does not pull it back in.
	retired := map[string]bool{settings.RetiredTasksPack: true}
	chosen, err := adopt.Resolve(in.Reader, declared.Declared, retired, adopt.PacksChannel(in.Channel), got.Version, in.Out)
	if err != nil {
		return err
	}
	held := map[string]bool{}
	for _, id := range declared.Declared {
		held[id] = true
	}
	for _, v := range chosen {
		if !held[v.ID] {
			if cfg, err = settings.AddDeclared(cfg, settings.YAML, v.ID); err != nil {
				return err
			}
		}
		fmt.Fprintf(in.Out, "pack: %s %s\n", v.ID, v.Entry.Version)
	}
	if _, err := settings.ParseFile(cfg, settings.YAML); err != nil {
		return fmt.Errorf("the imported settings do not read back: %w", err)
	}
	claudeSettings, err := movedHooks(filepath.Join(in.Repo, ".claude", "settings.json"))
	if err != nil {
		return err
	}
	ignore, err := withLine(filepath.Join(in.Repo, ".claudinite", ".gitignore"), "bin/")
	if err != nil {
		return err
	}
	name := in.FullName
	if name == "" {
		name = filepath.Base(in.Repo)
	}
	files := []memberFile{
		{".claudinite/launch", got.Launcher, 0o755},
		{".claudinite/settings.yaml", cfg, 0o644},
		{".claudinite/.gitignore", ignore, 0o644},
		{".claude/settings.json", claudeSettings, 0o644},
	}
	tmpl := workflows.ForRepo(name)
	for _, n := range workflows.Names {
		data := tmpl[n]
		if have, err := os.ReadFile(filepath.Join(in.Repo, ".github", "workflows", n)); err == nil {
			if data, err = movedWorkflow(n, have, name); err != nil {
				return err
			}
		}
		files = append(files, memberFile{".github/workflows/" + n, data, 0o644})
	}
	// The last check before the first write: a local pack cn would not
	// load, even rewritten, refuses the move with every file as it was.
	restore, err := applyOwnPackEdits(in.Repo, edits)
	if err != nil {
		return err
	}
	if err := ownPacksLoad(in.Repo, declared.Local); err != nil {
		restore()
		return err
	}
	for _, e := range edits {
		for _, w := range e.what {
			fmt.Fprintf(in.Out, "own pack: %s: %s\n", e.rel, w)
		}
	}
	if err := os.RemoveAll(filepath.Join(in.Repo, ".claudinite", "shared")); err != nil {
		return err
	}
	if err := writeFiles(in.Repo, files); err != nil {
		return halfMoved(in.Repo, err)
	}
	if err := adopt.EnsureSkillsIgnore(in.Repo); err != nil {
		return halfMoved(in.Repo, err)
	}
	for _, v := range chosen {
		if err := packs.Unpack(v.Archive, packset.Tree(in.Repo, v.ID)); err != nil {
			return halfMoved(in.Repo, err)
		}
	}
	if _, err := rulesindex.Converge(in.Repo, got.Version); err != nil {
		return halfMoved(in.Repo, err)
	}
	if _, err := rulesindex.EnsureImport(in.Repo); err != nil {
		return halfMoved(in.Repo, err)
	}
	err = adopt.Finish(adopt.FinishInput{Repo: in.Repo, Engine: got.Version, Answers: in.Answers, Core: true, NoSeed: true,
		First: []string{"git rm " + node.File + " (the Node declaration, now read into .claudinite/settings.yaml)"}, Out: in.Out})
	if err != nil {
		return halfMoved(in.Repo, err)
	}
	return nil
}

// ownPacksLoad refuses the move when a declared local pack would not
// load, since the rules index and the checks would leave it out.
func ownPacksLoad(repo string, local []string) error {
	var bad []string
	for _, name := range local {
		if _, err := packset.LoadLocal(repo, name); err != nil {
			bad = append(bad, fmt.Sprintf("%s%s: %v", settings.LocalPrefix, name, err))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("cn would not load the declared local pack %s; fix or undeclare it, and run the move again; the repo is as it was", strings.Join(bad, "; and "))
	}
	return nil
}

// halfMoved undoes what marks repo a cn member after the move failed part
// way, so a re-run is not refused, and names the restore for the rest.
func halfMoved(repo string, cause error) error {
	var left []string
	for _, rel := range []string{".claudinite/launch", ".claudinite/settings.yaml"} {
		if err := os.Remove(filepath.Join(repo, filepath.FromSlash(rel))); err != nil && !errors.Is(err, os.ErrNotExist) {
			left = append(left, rel)
		}
	}
	msg := "removed .claudinite/launch and .claudinite/settings.yaml"
	if len(left) > 0 {
		msg = "could not remove " + strings.Join(left, " and ")
	}
	return fmt.Errorf("%w; the move stopped part way: %s, and `git checkout -- .` restores the rest", cause, msg)
}

// movedSettings is the pinned settings file a moved member starts from:
// the engine block, then the imported packs block with the channel first,
// then the imported checks block.
func movedSettings(channel, version, integrity string, decl node.Decl) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(adopt.EngineBlock(channel, version, integrity))
	p := settings.NewOrdered()
	p.Set("channel", adopt.PacksChannel(channel))
	if decl.Packs != nil {
		for _, k := range decl.Packs.Keys() {
			if k == "channel" {
				continue
			}
			v, _ := decl.Packs.Get(k)
			p.Set(k, v)
		}
	}
	d := decl
	d.Packs = p
	return settings.SpliceBlocks(b.Bytes(), settings.YAML, d.Blocks())
}

// movedHooks is .claude/settings.json with every command that runs the
// Node engine's hooks removed and cn's six wirings merged in; a group left
// with no command goes, and the member's own commands survive.
func movedHooks(path string) ([]byte, error) {
	obj, err := adopt.ReadClaudeSettings(path)
	if err != nil {
		return nil, err
	}
	if hooks, ok := obj["hooks"].(map[string]any); ok {
		for event, v := range hooks {
			groups, _ := v.([]any)
			var kept []any
			for _, g := range groups {
				if g, ok := withoutNodeHooks(g); ok {
					kept = append(kept, g)
				}
			}
			if len(kept) == 0 {
				delete(hooks, event)
			} else {
				hooks[event] = kept
			}
		}
	}
	return adopt.MergeHooksInto(obj)
}

// withoutNodeHooks is group with its Node engine commands dropped, and
// whether any command remains; a group naming none is returned as it was.
func withoutNodeHooks(group any) (any, bool) {
	gm, ok := group.(map[string]any)
	if !ok {
		return group, true
	}
	list, ok := gm["hooks"].([]any)
	if !ok {
		return group, true
	}
	var kept []any
	for _, c := range list {
		cm, _ := c.(map[string]any)
		if cmd, _ := cm["command"].(string); nodeHook.MatchString(cmd) {
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == len(list) {
		return group, true
	}
	if len(kept) == 0 {
		return nil, false
	}
	gm["hooks"] = kept
	return gm, true
}

// nodeCron is the Node engine's scheduler cron line, single-quoted.
var nodeCron = regexp.MustCompile(`(?m)^    - cron: '([0-9, *]+)'$`)

// movedWorkflow is template n as the member name, holding have, carries
// it: Expected, with the Node scheduler's single-quoted cron read as cn's
// double-quoted one.
func movedWorkflow(n string, have []byte, name string) ([]byte, error) {
	return workflows.Expected(n, nodeCron.ReplaceAll(have, []byte(`    - cron: "$1"`)), name)
}

// withLine is the file at path with line appended when it lacks it, the
// line alone when the file is absent.
func withLine(path, line string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []byte(line + "\n"), nil
	}
	if err != nil {
		return nil, err
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == line {
			return raw, nil
		}
	}
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		raw = append(raw, '\n')
	}
	return append(raw, line+"\n"...), nil
}

// memberFile is one file init writes.
type memberFile struct {
	rel  string
	data []byte
	mode os.FileMode
}

func writeFiles(repo string, files []memberFile) error {
	for _, f := range files {
		p := filepath.Join(repo, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, f.data, f.mode); err != nil {
			return err
		}
		if err := os.Chmod(p, f.mode); err != nil {
			return err
		}
	}
	return nil
}
