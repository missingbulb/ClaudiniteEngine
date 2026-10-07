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

// ChannelStaging is the engine channel of quick linux-only builds, beside
// ChannelStable and ChannelCanary.
const ChannelStaging = "staging"

// EngineChannelPattern is engine.channel's values; the launcher reads no
// channel.
var EngineChannelPattern = regexp.MustCompile(`^(stable|canary|staging)$`)

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
	// Channel is the npm dist-tag the updater follows, by its channel name;
	// absent, it is stable.
	Channel    string
	HasChannel bool
}

// span is a byte range of the file holding one "line" of the engine block:
// a real line for YAML and TOML, a comma-separated piece of the engine
// object for JSON.
type span struct{ start, end int }

var (
	yamlEnd = regexp.MustCompile(`^[^ \t]`)
	tomlEnd = regexp.MustCompile(`^[ \t]*\[`)
)

// blockPatterns are the header patterns of one top-level block.
type blockPatterns struct {
	name                 string
	yamlHeader, tomlHead *regexp.Regexp
	jsonKey, jsonBlock   *regexp.Regexp
}

func patternsFor(name string) blockPatterns {
	q := regexp.QuoteMeta(name)
	return blockPatterns{
		name:       name,
		yamlHeader: regexp.MustCompile(`^` + q + `:[ \t]*$`),
		tomlHead:   regexp.MustCompile(`^\[` + q + `\][ \t]*$`),
		jsonKey:    regexp.MustCompile(`"` + q + `"[ \t]*:`),
		jsonBlock:  regexp.MustCompile(`"` + q + `"[ \t]*:[ \t]*\{([^{}]*)\}`),
	}
}

var (
	enginePatterns  = patternsFor("engine")
	licensePatterns = patternsFor("license")
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
	spans, _, err := namedBlockSpans(raw, f, enginePatterns, true)
	return spans, err
}

// namedBlockSpans returns a block's lines and whether the block is there;
// required refuses a file without exactly one, otherwise an absent block
// is no lines.
func namedBlockSpans(raw []byte, f Format, b blockPatterns, required bool) ([]span, bool, error) {
	switch f {
	case YAML, TOML:
		header, end := b.yamlHeader, yamlEnd
		if f == TOML {
			header, end = b.tomlHead, tomlEnd
		}
		found := 0
		var out []span
		in := false
		for _, s := range lineSpans(raw) {
			line := strings.TrimSuffix(string(raw[s.start:s.end]), "\r")
			switch {
			case header.MatchString(line):
				in = true
				found++
			case in && end.MatchString(line):
				in = false
			case in:
				out = append(out, s)
			}
		}
		if found > 1 {
			return nil, true, fmt.Errorf("the settings hold the %s block %d times", b.name, found)
		}
		return out, found == 1, nil
	case JSON:
		flat := strings.NewReplacer("\r", "", "\n", "").Replace(string(raw))
		n := len(b.jsonKey.FindAllString(flat, -1))
		if n == 0 && !required {
			return nil, false, nil
		}
		if n != 1 {
			return nil, n > 0, fmt.Errorf("the settings must hold exactly one %q object, found %d", b.name, n)
		}
		loc := b.jsonBlock.FindSubmatchIndex(raw)
		if loc == nil {
			return nil, true, fmt.Errorf("the %q object must hold only plain values", b.name)
		}
		var out []span
		start := loc[2]
		for i := loc[2]; i < loc[3]; i++ {
			if raw[i] == ',' {
				out = append(out, span{start, i})
				start = i + 1
			}
		}
		return append(out, span{start, loc[3]}), true, nil
	}
	return nil, false, fmt.Errorf("unknown settings format %q", f)
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
	return keyIn(raw, f, spans, "engine", key)
}

// keyIn finds key's one line among a block's spans.
func keyIn(raw []byte, f Format, spans []span, block, key string) (span, bool, error) {
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
		return span{}, true, fmt.Errorf("%s.%s appears %d times", block, key, len(hits))
	}
	m := value.FindSubmatchIndex(raw[hits[0].start:hits[0].end])
	if m == nil {
		return span{}, true, fmt.Errorf("%s.%s must be a plain quoted string", block, key)
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
	if err := get("version", VersionPattern, `version like "1.60928.1"`, &e.Version, true); err != nil {
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
	if err := get("channel", EngineChannelPattern, `"stable", "canary" or "staging"`, &e.Channel, false); err != nil {
		return Engine{}, err
	}
	e.HasChannel = e.Channel != ""
	switch {
	case e.HasChannel:
	case e.Package == LegacyCanaryPackage:
		e.Channel = ChannelCanary
	default:
		e.Channel = ChannelStable
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
// engine.manifest changed, both still valid, the retired license block
// dropped and a legacy package moved to its channel.
func PinOnlyChange(old, new []byte, f Format) error {
	err := pinOnlyChange(old, new, f)
	if err == nil {
		return nil
	}
	if bridged, ok, berr := FromLegacyPackage(old, f); berr == nil && ok && pinOnlyChange(bridged, new, f) == nil {
		return nil
	}
	return err
}

func pinOnlyChange(old, new []byte, f Format) error {
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
	if oe.Channel != ne.Channel || oe.HasChannel != ne.HasChannel {
		return errors.New("engine.channel changed")
	}
	moved, err := SetPin(old, f, ne.Version, ne.Manifest)
	if err != nil {
		return err
	}
	if string(moved) == string(new) {
		return nil
	}
	if dropped, err := DropLicense(moved, f); err == nil && string(dropped) == string(new) {
		return nil
	}
	return errors.New("the settings change touches more than engine.version and engine.manifest")
}
