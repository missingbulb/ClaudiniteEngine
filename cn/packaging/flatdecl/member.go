package flatdecl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// MemberFile is a cn member stating itself in one JSON file, for a reader
// with no YAML or TOML parser and no wish to read a manifest per pack:
// the settings file it keeps, its engine pin, its declared packs with
// their config, its dormancy and each vendored canon pack's version.
const MemberFile = Dir + "/member.GENERATED.json"

// MemberEngine is the pin as the member file states it.
type MemberEngine struct {
	Package string `json:"package"`
	Version string `json:"version"`
	Channel string `json:"channel"`
}

// MemberEntry is one declared pack: its id as declared and its config
// when the declaration carries one.
type MemberEntry struct {
	ID     string         `json:"id"`
	Config map[string]any `json:"config,omitempty"`
}

// Member is the member file. Engine is nil when the settings file holds
// no readable pin: unknown, never a zero version.
type Member struct {
	Version  int `json:"version"`
	Settings struct {
		Path   string `json:"path"`
		Format string `json:"format"`
	} `json:"settings"`
	Engine *MemberEngine `json:"engine"`
	Packs  struct {
		Channel  string        `json:"channel"`
		Declared []MemberEntry `json:"declared"`
	} `json:"packs"`
	Dormant bool              `json:"dormant"`
	Held    map[string]string `json:"held"`
}

// ReadMember is the member file's content for repo: false when the repo
// keeps no .claudinite/settings.* (an unadopted repo, or the shelf). packs
// are the loaded packs, whose canon versions are the held ones.
func ReadMember(repo string, packs []packset.Pack) (Member, bool, error) {
	path, f, err := settings.Find(repo)
	if err != nil {
		return Member{}, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Member{}, false, err
	}
	m := Member{Version: Version, Held: map[string]string{}}
	m.Settings.Path, m.Settings.Format = settings.RelPath(f), string(f)
	if e, err := settings.ReadEngine(raw, f); err == nil {
		m.Engine = &MemberEngine{Package: e.Package, Version: e.Version, Channel: e.Channel}
	}
	parsed, err := settings.ParseFile(raw, f)
	if err != nil {
		return Member{}, false, err
	}
	declared := parsed.Packs
	tasks, _ := parsed.TasksBlock()
	m.Dormant, _ = tasks[settings.DormantTasksKey].(bool)
	m.Packs.Channel = declared.Channel
	if m.Packs.Channel == "" {
		m.Packs.Channel = settings.ChannelStable
	}
	m.Packs.Declared = []MemberEntry{}
	for _, e := range declared.Entries {
		m.Packs.Declared = append(m.Packs.Declared, MemberEntry{ID: e.Token(), Config: e.Config})
	}
	for _, p := range packs {
		if p.Kind == packset.Canon && p.Version != "" {
			m.Held[p.ID] = p.Version
		}
	}
	return m, true, nil
}

// MemberContent is the member file's text: two-space JSON in a fixed key
// order, config objects by key, and a trailing newline, so a run that
// changes nothing writes the same bytes.
func MemberContent(m Member) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	return b.String(), nil
}

// WriteMember writes the member file alone, where the repo holds it
// (Held), when it changed, returning the path written or ""; a repo with
// no .claudinite/settings.* writes nothing.
func WriteMember(repo string, packs []packset.Pack) (string, error) {
	m, ok, err := ReadMember(repo, packs)
	if err != nil || !ok {
		return "", err
	}
	text, err := MemberContent(m)
	if err != nil {
		return "", err
	}
	rel := HeldIn(repo, MemberFile)
	p := filepath.Join(repo, filepath.FromSlash(rel))
	if old, err := os.ReadFile(p); err == nil && string(old) == text {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return rel, os.WriteFile(p, []byte(text), 0o644)
}
