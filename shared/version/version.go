// Package version carries the engine's version and platform and the
// <major>.<day>.<n> version format: <major> is the release line a person
// raises by hand, <day> the release's UTC date as Today numbers it, and
// <n> its build that day, from 1. Versions travel as strings and compare as
// numbers, major then day then build. A dev build's 0.0.0 is the one
// version with no day.
package version

import (
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// Set by the release build with -ldflags "-X ...version=... -X ...commit=...".
var (
	version = ""
	commit  = ""
)

// Version is the injected release version, or 0.0.0 for a dev build.
func Version() string {
	if version == "" {
		return "0.0.0"
	}
	return version
}

// Commit is the injected short commit, else the VCS revision Go stamped into
// a dev build, else "unknown".
func Commit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "unknown"
}

// V is a parsed version.
type V struct {
	Major, Day, Build uint64
}

// Parse reads <major>.<day>.<n>: three decimal numbers, no leading zeros,
// nothing else, with <day> a date Today could print and <n> from 1; 0.0.0
// is the dev build. A version in the retired <day>.<n>.0 format is refused
// by name.
func Parse(s string) (V, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return V{}, fmt.Errorf("version %q: want <major>.<day>.<n>, three dot-separated numbers", s)
	}
	var n [3]uint64
	for i, p := range parts {
		if !plain(p) {
			return V{}, fmt.Errorf("version %q: part %q is not a plain number", s, p)
		}
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return V{}, fmt.Errorf("version %q: %w", s, err)
		}
		n[i] = v
	}
	v := V{Major: n[0], Day: n[1], Build: n[2]}
	if v == (V{}) {
		return v, nil
	}
	if !isDay(v.Day) || v.Build == 0 {
		if isDay(v.Major) && v.Build == 0 {
			return V{}, fmt.Errorf("version %q is in the retired <day>.<n>.0 format; want <major>.<day>.<n>, as in 1.%d.%d", s, v.Major, v.Day)
		}
		if !isDay(v.Day) {
			return V{}, fmt.Errorf("version %q: %d is not a <day>, (year-2020)*10000 + month*100 + day", s, v.Day)
		}
		return V{}, fmt.Errorf("version %q: the build <n> counts from 1", s)
	}
	return v, nil
}

// plain reports whether p is a decimal number with no sign or leading zero.
func plain(p string) bool {
	return p != "" && (len(p) == 1 || p[0] != '0') && strings.TrimLeft(p, "0123456789") == ""
}

// isDay reports whether d has a month and a day of the month in Today's
// layout.
func isDay(d uint64) bool {
	month, day := d/100%100, d%100
	return month >= 1 && month <= 12 && day >= 1 && day <= 31
}

// String is v as Parse reads it.
func (v V) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Day, v.Build) }

// Compare returns -1, 0 or 1 as a sorts before, with or after b.
func Compare(a, b string) (int, error) {
	va, err := Parse(a)
	if err != nil {
		return 0, err
	}
	vb, err := Parse(b)
	if err != nil {
		return 0, err
	}
	for _, d := range [][2]uint64{{va.Major, vb.Major}, {va.Day, vb.Day}, {va.Build, vb.Build}} {
		switch {
		case d[0] < d[1]:
			return -1, nil
		case d[0] > d[1]:
			return 1, nil
		}
	}
	return 0, nil
}

// Today is the <day> part for t's UTC date: (year-2020)*10000 + month*100 + day.
func Today(t time.Time) int {
	t = t.UTC()
	return (t.Year()-2020)*10000 + int(t.Month())*100 + t.Day()
}

// Platforms are the five platforms a release carries, in manifest order.
var Platforms = []string{"linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64", "windows-x64"}

// PlatformName maps a GOOS/GOARCH pair to its release platform name, or "".
func PlatformName(goos, goarch string) string {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if arch == "" {
		return ""
	}
	switch goos {
	case "linux", "darwin":
		return goos + "-" + arch
	case "windows":
		if arch == "x64" {
			return "windows-x64"
		}
	}
	return ""
}

// Platform is the running binary's platform name.
func Platform() string {
	if p := PlatformName(runtime.GOOS, runtime.GOARCH); p != "" {
		return p
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// MinEngine is a pack's parsed minEngineVersion.
type MinEngine struct{ v V }

// ErrNodeEngine is a two-part minEngineVersion: the Node engine's own
// version, which names no cn release.
//
// @legacy-tolerance advisory:pack-min-engine retire:#18
var ErrNodeEngine = errors.New("names a Node engine version")

// ParseMinEngineVersion reads a pack's minEngineVersion, <major>.<day>.<n>
// as Parse reads it. A two-part value is ErrNodeEngine; anything else
// unreadable is refused.
func ParseMinEngineVersion(s string) (MinEngine, error) {
	v, err := Parse(s)
	if err == nil {
		return MinEngine{v: v}, nil
	}
	if p := strings.Split(s, "."); len(p) == 2 && plain(p[0]) && plain(p[1]) {
		return MinEngine{}, fmt.Errorf("minEngineVersion %q %w; want <major>.<day>.<n>", s, ErrNodeEngine)
	}
	return MinEngine{}, fmt.Errorf("minEngineVersion %q: %w", s, err)
}

// Satisfies reports whether an engine pinned at pin meets the minimum; an
// unreadable pin meets nothing.
func (m MinEngine) Satisfies(pin string) bool {
	c, err := Compare(pin, m.v.String())
	return err == nil && c >= 0
}
