// Package adopt is cn init and cn adopt: the member files, the pack
// declaration and the first vendoring. Init adopts a repo from nothing:
// it picks the newest allowed engine version, fetches and verifies it and
// runs its selftest, reads and verifies every declared pack (plus what
// they require), and only when every read succeeded writes the files. No
// adoption questions or seed operations run yet (phase 6).
package adopt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/verify"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// DefaultPackage is the engine package a pin names when it names none.
const DefaultPackage = "@claudinite/cli"

// Input is one cn init.
type Input struct {
	Repo string
	// FullName is the repo's "owner/name", which the scheduler's cron is
	// hashed from; "" hashes the directory's name.
	FullName string
	Packs    []string
	Channel  string
	Package  string
	// Fetch is the engine fetch's registry, roots, cache and platform;
	// Package, Version and Packument are filled in here.
	Fetch   update.FetchInput
	Reader  update.PackReader
	Timeout time.Duration
	Out     io.Writer
	// Key makes init's one key request for the repo, in the foreground
	// within the cut; nil makes none.
	Key func(repo string) KeyGrant
}

// KeyGrant is what init's key request came to: the plan the key names,
// or why no key came and the link that wants.
type KeyGrant struct {
	Plan   string
	Reason string
	Link   string
}

// vendored is one pack chosen and read, ready to unpack.
type vendored struct {
	id      string
	entry   packindex.Entry
	archive []byte
}

// resolve selects every id and what their requires pull in, in order,
// for an engine at pin on channel, and reads each archive. held names the
// packs already declared, which are not vendored again.
func resolve(r update.PackReader, ids []string, held map[string]bool, channel, pin string, out io.Writer) ([]vendored, error) {
	queue := append([]string{}, ids...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	var got []vendored
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		v, err := r.VerifiedIndex(id)
		if err != nil {
			return nil, err
		}
		c := packindex.Select(v.Index, packindex.Want{Channel: channel, Engine: pin})
		if c.Entry == nil {
			why := "it lists no version"
			if c.Skipped != nil {
				why = fmt.Sprintf("its newest, %s, is skipped: %s", c.Skipped.Version, c.Skipped.Reason)
			}
			return nil, fmt.Errorf("pack %s has no version for engine %s on the %s channel: %s", id, pin, channel, why)
		}
		for _, req := range c.Entry.Requires {
			if !seen[req] && !held[req] {
				seen[req] = true
				queue = append(queue, req)
				fmt.Fprintf(out, "adding pack %s, which %s %s requires\n", req, id, c.Entry.Version)
			}
		}
		data, err := r.Archive(id, *c.Entry)
		if err != nil {
			return nil, err
		}
		got = append(got, vendored{id: id, entry: *c.Entry, archive: data})
	}
	return got, nil
}

func checkIDs(ids []string) error {
	if len(ids) == 0 {
		return errors.New("name at least one pack")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !settings.PackIDPattern.MatchString(id) {
			return fmt.Errorf("%q is not a pack id", id)
		}
		if seen[id] {
			return fmt.Errorf("pack %s is named twice", id)
		}
		seen[id] = true
	}
	return nil
}

// Init adopts in.Repo and ends on the adoption checklist. Every write
// happens after every read succeeded.
func Init(in Input) error {
	for _, f := range settings.Formats {
		if _, err := os.Stat(filepath.Join(in.Repo, filepath.FromSlash(settings.RelPath(f)))); err == nil {
			return fmt.Errorf("%s already exists: this repo is adopted; use cn adopt <pack> to declare a pack", settings.RelPath(f))
		}
	}
	if _, err := os.Stat(filepath.Join(in.Repo, ".claudinite", "launch")); err == nil {
		return errors.New(".claudinite/launch already exists: this repo is adopted")
	}
	if in.Channel == "" {
		in.Channel = settings.ChannelStable
	}
	if in.Channel != settings.ChannelStable && in.Channel != settings.ChannelCanary {
		return fmt.Errorf("channel %q: want stable or canary", in.Channel)
	}
	if in.Package == "" {
		in.Package = DefaultPackage
	}
	if in.Package != "@claudinite/cli" && in.Package != "@claudinite/cli-rc" {
		return fmt.Errorf("package %q: want @claudinite/cli or @claudinite/cli-rc", in.Package)
	}
	if err := checkIDs(in.Packs); err != nil {
		return err
	}

	p, err := in.Fetch.Registry.Packument(in.Package)
	if err != nil {
		return fmt.Errorf("%s from %s: %w", in.Package, in.Fetch.Registry.Registry, err)
	}
	c := update.Candidate("0.0.0", p, update.StatesFromPackument(p))
	if c.Version == "" {
		return fmt.Errorf("%s on %s has no version that is not deprecated, held or revoked", in.Package, in.Fetch.Registry.Registry)
	}
	fi := in.Fetch
	fi.Package, fi.Version, fi.Packument = in.Package, c.Version, p
	got, err := update.Fetch(fi)
	if err != nil {
		return err
	}
	if len(got.Launcher) == 0 {
		return fmt.Errorf("%s %s carries no launcher (package/launch)", in.Package, c.Version)
	}
	self, err := update.Selftest(got.Binary, c.Version, in.Timeout)
	if err != nil {
		return err
	}
	fmt.Fprintf(in.Out, "engine: %s %s (manifest %s, key %s)\n", in.Package, c.Version, got.Integrity, got.KeyID)
	fmt.Fprint(in.Out, self)
	chosen, err := resolve(in.Reader, in.Packs, nil, in.Channel, c.Version, in.Out)
	if err != nil {
		return err
	}
	var ids []string
	for _, v := range chosen {
		ids = append(ids, v.id)
		fmt.Fprintf(in.Out, "pack: %s %s\n", v.id, v.entry.Version)
	}

	var cfg bytes.Buffer
	cfg.WriteString("engine:\n")
	if in.Package != DefaultPackage {
		fmt.Fprintf(&cfg, "  package: %q\n", in.Package)
	}
	fmt.Fprintf(&cfg, "  version: %q\n  manifest: %q\npacks:\n  channel: %q\n  declared:\n", got.Version, got.Integrity, in.Channel)
	for _, id := range ids {
		fmt.Fprintf(&cfg, "    - %s\n", id)
	}
	claudeSettings, err := mergeHooks(filepath.Join(in.Repo, ".claude", "settings.json"))
	if err != nil {
		return err
	}
	files := []struct {
		rel  string
		data []byte
		mode os.FileMode
	}{
		{".claudinite/launch", got.Launcher, 0o755},
		{".claudinite/settings.yaml", cfg.Bytes(), 0o644},
		{".claudinite/.gitignore", []byte("bin/\n"), 0o644},
		{SkillsIgnore, []byte(skillsIgnoreBody), 0o644},
		{".claude/settings.json", claudeSettings, 0o644},
	}
	name := in.FullName
	if name == "" {
		name = filepath.Base(in.Repo)
	}
	tmpl := workflows.ForRepo(name)
	for _, n := range workflows.Names {
		files = append(files, struct {
			rel  string
			data []byte
			mode os.FileMode
		}{".github/workflows/" + n, tmpl[n], 0o644})
	}
	for _, f := range files {
		p := filepath.Join(in.Repo, filepath.FromSlash(f.rel))
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
	for _, v := range chosen {
		if err := packs.Unpack(v.archive, packset.Tree(in.Repo, v.id)); err != nil {
			return err
		}
	}
	if _, err := rulesindex.Converge(in.Repo, got.Version); err != nil {
		return err
	}
	if _, err := rulesindex.EnsureImport(in.Repo); err != nil {
		return err
	}
	g := KeyGrant{Reason: "no key request"}
	if in.Key != nil {
		g = in.Key(in.Repo)
	}
	if g.Plan != "" {
		p := filepath.Join(in.Repo, ".claudinite", "settings.yaml")
		moved, err := settings.SetPlan(cfg.Bytes(), settings.YAML, g.Plan)
		if err != nil {
			return err
		}
		if err := os.WriteFile(p, moved, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(in.Out, `
Adopted. Next:
  1. Commit everything above and open a pull request; a person merges it, since it adds workflows.
  2. In the repository's Settings > Actions > General, allow GitHub Actions to create and approve pull requests, so the nightly update can open its PRs.
`)
	if g.Plan != "" {
		fmt.Fprintf(in.Out, "  3. The Claudinite App answered with this repo's license key; the settings file now names its plan.\nplan: %s\n", g.Plan)
		return nil
	}
	link := g.Link
	if link == "" {
		link = InstallURL
	}
	fmt.Fprintf(in.Out, "  3. No license key came (%s): the Claudinite GitHub App is not installed on this repo or not reachable, and sessions run degraded until an owner installs it:\n     %s\n", g.Reason, link)
	return nil
}

// InstallURL is the Claudinite App's install page; the license package's
// own constant names the same App (a drift guard in cmd/cn's tests keeps
// the two equal).
const InstallURL = "https://github.com/apps/claudinite/installations/new"

// mergeHooks returns .claude/settings.json with the six hook wirings
// added where missing, creating the object when the file is absent; keys
// cn does not own survive (re-serialized, the one Claude Code file cn
// init rewrites).
func mergeHooks(path string) ([]byte, error) {
	obj := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf(".claude/settings.json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	hooks, _ := obj["hooks"].(map[string]any)
	if hooks == nil {
		if _, present := obj["hooks"]; present {
			return nil, errors.New(".claude/settings.json: hooks is not an object")
		}
		hooks = map[string]any{}
	}
	for _, h := range verify.Hooks {
		groups, _ := hooks[h.Event].([]any)
		wired := false
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			list, _ := gm["hooks"].([]any)
			for _, c := range list {
				cm, _ := c.(map[string]any)
				cmd, _ := cm["command"].(string)
				wired = wired || strings.TrimSpace(cmd) == h.Command
			}
		}
		if wired {
			continue
		}
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": h.Command}}}
		if h.Event == "PreToolUse" || h.Event == "PostToolUse" {
			group["matcher"] = "*"
		}
		hooks[h.Event] = append(groups, group)
	}
	obj["hooks"] = hooks
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// AdoptInput is one cn adopt.
type AdoptInput struct {
	Repo   string
	ID     string
	Reader update.PackReader
	Out    io.Writer
}

// SkillsIgnore keeps the skills SessionStart mounts out of git.
const SkillsIgnore = ".claude/skills/.gitignore"

const skillsIgnoreBody = "*\n!.gitignore\n"

// ensureSkillsIgnore writes SkillsIgnore when the repo has none.
func ensureSkillsIgnore(repo string) error {
	path := filepath.Join(repo, filepath.FromSlash(SkillsIgnore))
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(skillsIgnoreBody), 0o644)
}

// Adopt declares one more pack on an adopted repo, with what it requires,
// and vendors them for the pinned engine; it regenerates the rules index
// and writes what a member adopted before them lacks: the CLAUDE.md
// import and the skills ignore. Skills mount at the next SessionStart.
func Adopt(in AdoptInput) error {
	if err := checkIDs([]string{in.ID}); err != nil {
		return err
	}
	path, f, err := settings.Find(in.Repo)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pin, err := settings.ReadEngine(raw, f)
	if err != nil {
		return err
	}
	declared, err := settings.ReadPacks(raw, f)
	if err != nil {
		return err
	}
	held := map[string]bool{}
	for _, id := range declared.Declared {
		held[id] = true
	}
	if held[in.ID] {
		return fmt.Errorf("pack %s is already declared", in.ID)
	}
	chosen, err := resolve(in.Reader, []string{in.ID}, held, declared.Channel, pin.Version, in.Out)
	if err != nil {
		return err
	}
	for _, v := range chosen {
		if raw, err = settings.AddDeclared(raw, f, v.id); err != nil {
			return err
		}
	}
	for _, v := range chosen {
		if err := packs.Unpack(v.archive, packset.Tree(in.Repo, v.id)); err != nil {
			return err
		}
		fmt.Fprintf(in.Out, "pack: %s %s\n", v.id, v.entry.Version)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	if _, err := rulesindex.Converge(in.Repo, pin.Version); err != nil {
		return err
	}
	if _, err := rulesindex.EnsureImport(in.Repo); err != nil {
		return err
	}
	if err := ensureSkillsIgnore(in.Repo); err != nil {
		return err
	}
	fmt.Fprintf(in.Out, "Declared in %s; commit it with %s/, %s, %s and %s and open a pull request.\n", settings.RelPath(f), packset.Dir, rulesindex.File, rulesindex.ClaudeMD, SkillsIgnore)
	return nil
}
