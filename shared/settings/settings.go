// Package settings reads and edits the engine pin in a member's
// .claudinite/settings.yaml, .toml or .json the way the launcher reads it:
// no parser, a strict pattern per key inside the engine block, so the
// launcher, the updater and verify agree on every file. The pin moves by a
// line edit, never by re-serializing, so a member's comments and key order
// survive.
package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Format is the settings file's extension.
type Format string

const (
	YAML Format = "yaml"
	TOML Format = "toml"
	JSON Format = "json"
)

// Formats in the launcher's search order.
var Formats = []Format{YAML, TOML, JSON}

// DefaultPackage is the channel a pin without engine.package reads.
const DefaultPackage = "@claudinite/cli"

// The launcher's strict patterns.
var (
	VersionPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	ManifestPattern = regexp.MustCompile(`^sha512-[A-Za-z0-9+/]{86}==$`)
	PackagePattern  = regexp.MustCompile(`^@claudinite/cli(-rc)?$`)
)

// RelPath is a settings file's path relative to the repo root.
func RelPath(f Format) string { return ".claudinite/settings." + string(f) }

// Find returns the one settings file under repo, refusing none or several.
func Find(repo string) (string, Format, error) {
	var found []Format
	for _, f := range Formats {
		if st, err := os.Stat(filepath.Join(repo, filepath.FromSlash(RelPath(f)))); err == nil && st.Mode().IsRegular() {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		return "", "", fmt.Errorf("exactly one of .claudinite/settings.yaml, .claudinite/settings.toml or .claudinite/settings.json must exist, found %d", len(found))
	}
	return filepath.Join(repo, filepath.FromSlash(RelPath(found[0]))), found[0], nil
}

// Engine is the pin.
type Engine struct {
	Version, Manifest, Package string
	// HasPackage is false when engine.package is absent and Package is the
	// default.
	HasPackage bool
}

// span is a byte range of the file holding one "line" of the engine block:
// a real line for YAML and TOML, a comma-separated piece of the engine
// object for JSON.
type span struct{ start, end int }

var (
	yamlHeader = regexp.MustCompile(`^engine:[ \t]*$`)
	yamlEnd    = regexp.MustCompile(`^[^ \t]`)
	tomlHeader = regexp.MustCompile(`^\[engine\][ \t]*$`)
	tomlEnd    = regexp.MustCompile(`^[ \t]*\[`)
	jsonEngine = regexp.MustCompile(`"engine"[ \t]*:`)
	jsonBlock  = regexp.MustCompile(`"engine"[ \t]*:[ \t]*\{([^{}]*)\}`)
)

func lineSpans(raw []byte) []span {
	var out []span
	start := 0
	for i, c := range raw {
		if c == '\n' {
			out = append(out, span{start, i})
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, span{start, len(raw)})
	}
	return out
}

// blockSpans returns the engine block's lines.
func blockSpans(raw []byte, f Format) ([]span, error) {
	switch f {
	case YAML, TOML:
		header, end := yamlHeader, yamlEnd
		if f == TOML {
			header, end = tomlHeader, tomlEnd
		}
		var out []span
		in := false
		for _, s := range lineSpans(raw) {
			line := strings.TrimSuffix(string(raw[s.start:s.end]), "\r")
			switch {
			case header.MatchString(line):
				in = true
			case in && end.MatchString(line):
				in = false
			case in:
				out = append(out, s)
			}
		}
		return out, nil
	case JSON:
		flat := strings.NewReplacer("\r", "", "\n", "").Replace(string(raw))
		if n := len(jsonEngine.FindAllString(flat, -1)); n != 1 {
			return nil, fmt.Errorf("the settings must hold exactly one \"engine\" object, found %d", n)
		}
		loc := jsonBlock.FindSubmatchIndex(raw)
		if loc == nil {
			return nil, errors.New("the \"engine\" object must hold only plain values")
		}
		var out []span
		start := loc[2]
		for i := loc[2]; i < loc[3]; i++ {
			if raw[i] == ',' {
				out = append(out, span{start, i})
				start = i + 1
			}
		}
		return append(out, span{start, loc[3]}), nil
	}
	return nil, fmt.Errorf("unknown settings format %q", f)
}

func keyPatterns(f Format, key string) (*regexp.Regexp, *regexp.Regexp) {
	sep := ":"
	if f == TOML {
		sep = "="
	}
	q := regexp.QuoteMeta(key)
	present := regexp.MustCompile(`^[ \t\r\n]*"?` + q + `"?[ \t]*` + sep)
	value := regexp.MustCompile(`^([ \t\r\n]*"?` + q + `"?[ \t]*` + sep + `[ \t]*")([^"]*)("[ \t\r\n]*)$`)
	return present, value
}

// keyValue finds key's one line in the block: its span, the value's byte
// range, and whether it is present at all.
func keyValue(raw []byte, f Format, key string) (span, bool, error) {
	spans, err := blockSpans(raw, f)
	if err != nil {
		return span{}, false, err
	}
	present, value := keyPatterns(f, key)
	var hits []span
	for _, s := range spans {
		if present.Match(raw[s.start:s.end]) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return span{}, false, nil
	case 1:
	default:
		return span{}, true, fmt.Errorf("engine.%s appears %d times", key, len(hits))
	}
	m := value.FindSubmatchIndex(raw[hits[0].start:hits[0].end])
	if m == nil {
		return span{}, true, fmt.Errorf("engine.%s must be a plain quoted string", key)
	}
	return span{hits[0].start + m[4], hits[0].start + m[5]}, true, nil
}

// ReadEngine reads and validates the pin.
func ReadEngine(raw []byte, f Format) (Engine, error) {
	var e Engine
	get := func(key string, pattern *regexp.Regexp, want string, dst *string, required bool) error {
		v, ok, err := keyValue(raw, f, key)
		if err != nil {
			return err
		}
		if !ok {
			if required {
				return fmt.Errorf("engine.%s is missing; it must be a quoted %s", key, want)
			}
			return nil
		}
		*dst = string(raw[v.start:v.end])
		if !pattern.MatchString(*dst) {
			return fmt.Errorf("engine.%s must be a quoted %s, not %q", key, want, *dst)
		}
		return nil
	}
	if err := get("version", VersionPattern, `version like "60928.1.0"`, &e.Version, true); err != nil {
		return Engine{}, err
	}
	if err := get("manifest", ManifestPattern, "sha512-... integrity string", &e.Manifest, true); err != nil {
		return Engine{}, err
	}
	if err := get("package", PackagePattern, `"@claudinite/cli" or "@claudinite/cli-rc"`, &e.Package, false); err != nil {
		return Engine{}, err
	}
	e.HasPackage = e.Package != ""
	if !e.HasPackage {
		e.Package = DefaultPackage
	}
	return e, nil
}

// SetPin rewrites the values of engine.version and engine.manifest in
// place and changes no other byte.
func SetPin(raw []byte, f Format, version, manifest string) ([]byte, error) {
	if !VersionPattern.MatchString(version) || !ManifestPattern.MatchString(manifest) {
		return nil, fmt.Errorf("refusing to pin %q %q: not a version and an integrity string", version, manifest)
	}
	if _, err := ReadEngine(raw, f); err != nil {
		return nil, err
	}
	vs, _, _ := keyValue(raw, f, "version")
	ms, _, _ := keyValue(raw, f, "manifest")
	type edit struct {
		s   span
		val string
	}
	edits := []edit{{vs, version}, {ms, manifest}}
	if ms.start < vs.start {
		edits[0], edits[1] = edits[1], edits[0]
	}
	var b strings.Builder
	at := 0
	for _, e := range edits {
		b.Write(raw[at:e.s.start])
		b.WriteString(e.val)
		at = e.s.end
	}
	b.Write(raw[at:])
	return []byte(b.String()), nil
}

// PinOnlyChange refuses unless new is old with at most engine.version and
// engine.manifest changed, both still valid.
func PinOnlyChange(old, new []byte, f Format) error {
	oe, err := ReadEngine(old, f)
	if err != nil {
		return fmt.Errorf("the base settings: %w", err)
	}
	ne, err := ReadEngine(new, f)
	if err != nil {
		return err
	}
	if oe.Package != ne.Package || oe.HasPackage != ne.HasPackage {
		return errors.New("engine.package changed")
	}
	moved, err := SetPin(old, f, ne.Version, ne.Manifest)
	if err != nil {
		return err
	}
	if string(moved) != string(new) {
		return errors.New("the settings change touches more than engine.version and engine.manifest")
	}
	return nil
}
