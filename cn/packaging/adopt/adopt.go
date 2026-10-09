// Package adopt is cn init and cn adopt: the member files, the pack
// declaration and the first vendoring. Init adopts a repo from nothing:
// it picks the newest allowed engine version, fetches and verifies it and
// runs its selftest, reads and verifies every declared pack (plus what
// they require), and only when every read succeeded writes the files; it
// then records any answers given, seeds and stamps for the new packs and
// prints what is left: the questions, the human steps and the next one.
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

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/rules/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/fetch"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/interview"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/update"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/verify"
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
	// Channel is the engine channel, stable, canary or staging; the packs
	// read staging's engine beside canary packs.
	Channel string
	// Fetch is the engine fetch's registry, roots, cache and platform;
	// Package, Version and Packument are filled in here.
	Fetch   update.FetchInput
	Reader  update.PackReader
	Timeout time.Duration
	Out     io.Writer
	// Answers are recorded once the packs are declared.
	Answers []AnswerFlag
}

// Vendored is one pack chosen and read, ready to unpack.
type Vendored struct {
	ID      string
	Entry   packindex.Entry
	Archive []byte
}

// Resolve selects every id and what their requires pull in, in order,
// for an engine at pin on channel, and reads each archive. held names the
// packs already declared, which are not vendored again.
func Resolve(r update.PackReader, ids []string, held map[string]bool, channel, pin string, out io.Writer) ([]Vendored, error) {
	queue := append([]string{}, ids...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	var got []Vendored
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
		got = append(got, Vendored{ID: id, Entry: *c.Entry, Archive: data})
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
	if err := checkIDs(in.Packs); err != nil {
		return err
	}
	got, err := PickEngine(&in)
	if err != nil {
		return err
	}
	chosen, err := Resolve(in.Reader, in.Packs, nil, PacksChannel(in.Channel), got.Version, in.Out)
	if err != nil {
		return err
	}
	var ids []string
	for _, v := range chosen {
		ids = append(ids, v.ID)
		fmt.Fprintf(in.Out, "pack: %s %s\n", v.ID, v.Entry.Version)
	}

	var cfg bytes.Buffer
	cfg.WriteString(EngineBlock(in.Channel, got.Version, got.Integrity))
	fmt.Fprintf(&cfg, "packs:\n  channel: %q\n  declared:\n", PacksChannel(in.Channel))
	for _, id := range ids {
		fmt.Fprintf(&cfg, "    - %s\n", id)
	}
	claudeSettings, err := mergeHooks(filepath.Join(in.Repo, ".claude", "settings.json"))
	if err != nil {
		return err
	}
	files := []memberFile{
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
		files = append(files, memberFile{".github/workflows/" + n, tmpl[n], 0o644})
	}
	if err := writeFiles(in.Repo, files); err != nil {
		return err
	}
	for _, v := range chosen {
		if err := fetch.Unpack(v.Archive, packset.Tree(in.Repo, v.ID)); err != nil {
			return err
		}
	}
	if _, err := rulesindex.Converge(in.Repo, got.Version); err != nil {
		return err
	}
	if _, err := rulesindex.EnsureImport(in.Repo); err != nil {
		return err
	}
	return Finish(FinishInput{Repo: in.Repo, Engine: got.Version, Newly: ids, Answers: in.Answers, Core: true, Out: in.Out})
}

// EngineBlock is the pin a new member starts from, naming its channel
// unless it is stable.
func EngineBlock(channel, version, integrity string) string {
	var b strings.Builder
	b.WriteString("engine:\n")
	if channel != settings.ChannelStable {
		fmt.Fprintf(&b, "  channel: %q\n", channel)
	}
	fmt.Fprintf(&b, "  version: %q\n  manifest: %q\n", version, integrity)
	return b.String()
}

// PacksChannel is the pack channel beside an engine channel: staging's
// quick engines read the canary packs.
func PacksChannel(engine string) string {
	if engine == settings.ChannelStaging {
		return settings.ChannelCanary
	}
	return engine
}

// PickEngine settles in's channel, then picks, fetches and selftests the
// newest engine the channel's tags allow, printing what it chose.
func PickEngine(in *Input) (update.Fetched, error) {
	if in.Channel == "" {
		in.Channel = settings.ChannelStable
	}
	if !settings.EngineChannelPattern.MatchString(in.Channel) {
		return update.Fetched{}, fmt.Errorf("channel %q: want stable, canary or staging", in.Channel)
	}
	pkg := DefaultPackage
	p, err := in.Fetch.Registry.Packument(pkg)
	if err != nil {
		return update.Fetched{}, fmt.Errorf("%s from %s: %w", pkg, in.Fetch.Registry.Registry, err)
	}
	c := update.Candidate("0.0.0", in.Channel, p, update.StatesFromPackument(p))
	if c.Version == "" {
		return update.Fetched{}, fmt.Errorf("%s on %s has no %s version that is not deprecated, held or revoked", pkg, in.Fetch.Registry.Registry, in.Channel)
	}
	fi := in.Fetch
	fi.Package, fi.Version, fi.Packument = pkg, c.Version, p
	got, err := update.Fetch(fi)
	if err != nil {
		return update.Fetched{}, err
	}
	if len(got.Launcher) == 0 {
		return update.Fetched{}, fmt.Errorf("%s %s carries no launcher (package/launch)", pkg, c.Version)
	}
	self, err := update.Selftest(got.Binary, c.Version, "", in.Timeout)
	if err != nil {
		return update.Fetched{}, err
	}
	fmt.Fprintf(in.Out, "engine: %s %s (manifest %s, key %s)\n", pkg, c.Version, got.Integrity, got.KeyID)
	fmt.Fprint(in.Out, self)
	return got, nil
}

// FinishInput is the tail init and adopt share once the packs are
// vendored and declared.
type FinishInput struct {
	Repo, Engine string
	// Newly are the packs this run vendored, in the order it chose them.
	Newly   []string
	Answers []AnswerFlag
	// Core adds the core HANDOVER rows.
	Core bool
	// First are NEXT's steps before the commit's.
	First []string
	// NoSeed leaves the new packs' seedOps unwritten.
	NoSeed bool
	Out    io.Writer
}

// Finish records the answers, seeds and stamps for the new packs, and
// prints QUESTIONS, HANDOVER and NEXT.
func Finish(in FinishInput) error {
	if err := applyAnswers(in.Repo, in.Engine, in.Answers, in.Out); err != nil {
		return err
	}
	if !in.NoSeed {
		if err := seedAll(in.Repo, in.Newly, in.Out); err != nil {
			return err
		}
	}
	set, err := packset.Load(in.Repo, in.Engine, false)
	if err != nil {
		return err
	}
	newly := map[string]bool{}
	for _, id := range in.Newly {
		newly[id] = true
	}
	if err := stampExecutor(in.Repo, packSecrets(set, newly), in.Out); err != nil {
		return err
	}
	var packs []packset.Pack
	for _, id := range in.Newly {
		for _, p := range set.Packs {
			if p.Kind == packset.Canon && p.ID == id {
				packs = append(packs, p)
			}
		}
	}
	pending, _ := interview.State(set)
	steps := Handover(HandoverInput{Core: in.Core, Newly: packs})
	writeQuestions(in.Out, pending)
	writeHandover(in.Out, steps)
	writeNext(in.Out, NextInput{First: in.First, Routine: in.Core, Handover: len(steps) > 0})
	return nil
}

// mergeHooks returns .claude/settings.json with the six hook wirings
// added where missing, creating the object when the file is absent; keys
// cn does not own survive (re-serialized, the one Claude Code file cn
// init rewrites).
func mergeHooks(path string) ([]byte, error) {
	obj, err := ReadClaudeSettings(path)
	if err != nil {
		return nil, err
	}
	return MergeHooksInto(obj)
}

// ReadClaudeSettings is .claude/settings.json as an object, empty when
// the file is absent.
func ReadClaudeSettings(path string) (map[string]any, error) {
	obj := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf(".claude/settings.json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return obj, nil
}

// MergeHooksInto adds the six wirings to obj where missing and serializes
// it.
func MergeHooksInto(obj map[string]any) ([]byte, error) {
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
	Repo    string
	IDs     []string
	Answers []AnswerFlag
	Reader  update.PackReader
	Out     io.Writer
}

// SkillsIgnore keeps the skills SessionStart mounts out of git.
const SkillsIgnore = ".claude/skills/.gitignore"

const skillsIgnoreBody = "*\n!.gitignore\n"

// EnsureSkillsIgnore writes SkillsIgnore when the repo has none.
func EnsureSkillsIgnore(repo string) error {
	path := filepath.Join(repo, filepath.FromSlash(SkillsIgnore))
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(skillsIgnoreBody), 0o644)
}

// Adopt declares more packs on an adopted repo, with what they require,
// and vendors them for the pinned engine once every one resolved; it
// regenerates the indexes, writes what a member adopted before them lacks
// (the CLAUDE.md import, the skills ignore), and ends as init does for
// what it added. Skills mount at the next SessionStart.
func Adopt(in AdoptInput) error {
	if err := checkIDs(in.IDs); err != nil {
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
	for _, id := range in.IDs {
		if held[id] {
			return fmt.Errorf("pack %s is already declared", id)
		}
	}
	chosen, err := Resolve(in.Reader, in.IDs, held, declared.Channel, pin.Version, in.Out)
	if err != nil {
		return err
	}
	var ids []string
	for _, v := range chosen {
		if raw, err = settings.AddDeclared(raw, f, v.ID); err != nil {
			return err
		}
		ids = append(ids, v.ID)
	}
	for _, v := range chosen {
		if err := fetch.Unpack(v.Archive, packset.Tree(in.Repo, v.ID)); err != nil {
			return err
		}
		fmt.Fprintf(in.Out, "pack: %s %s\n", v.ID, v.Entry.Version)
	}
	if err := writeKeepingMode(path, raw); err != nil {
		return err
	}
	if _, err := rulesindex.Converge(in.Repo, pin.Version); err != nil {
		return err
	}
	if _, err := rulesindex.EnsureImport(in.Repo); err != nil {
		return err
	}
	if err := EnsureSkillsIgnore(in.Repo); err != nil {
		return err
	}
	fmt.Fprintf(in.Out, "Declared in %s.\n", settings.RelPath(f))
	return Finish(FinishInput{Repo: in.Repo, Engine: pin.Version, Newly: ids, Answers: in.Answers, Out: in.Out})
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
