// Package version carries the engine's version and platform and the
// <day>.<n>.0 version format. Versions travel as strings and compare as
// numbers.
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
	Day, Ordinal, Patch uint64
}

// Parse reads <day>.<n>.<patch>: three decimal numbers, no leading zeros,
// nothing else.
func Parse(s string) (V, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return V{}, fmt.Errorf("version %q: want three dot-separated numbers", s)
	}
	var n [3]uint64
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') || strings.TrimLeft(p, "0123456789") != "" {
			return V{}, fmt.Errorf("version %q: part %q is not a plain number", s, p)
		}
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return V{}, fmt.Errorf("version %q: %w", s, err)
		}
		n[i] = v
	}
	return V{Day: n[0], Ordinal: n[1], Patch: n[2]}, nil
}

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
	for _, d := range [][2]uint64{{va.Day, vb.Day}, {va.Ordinal, vb.Ordinal}, {va.Patch, vb.Patch}} {
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

// ParseMinEngineVersion reads a pack's minEngineVersion, <day>.<n>.<patch>
// as Parse reads it. A two-part value is ErrNodeEngine; anything else
// unreadable is refused.
func ParseMinEngineVersion(s string) (MinEngine, error) {
	v, err := Parse(s)
	if err == nil {
		return MinEngine{v: v}, nil
	}
	if _, e := Parse(s + ".0"); e == nil {
		return MinEngine{}, fmt.Errorf("minEngineVersion %q %w; want <day>.<n>.<patch>", s, ErrNodeEngine)
	}
	return MinEngine{}, fmt.Errorf("minEngineVersion %q: want <day>.<n>.<patch>", s)
}

// Satisfies reports whether an engine pinned at pin meets the minimum; an
// unreadable pin meets nothing.
func (m MinEngine) Satisfies(pin string) bool {
	c, err := Compare(pin, fmt.Sprintf("%d.%d.%d", m.v.Day, m.v.Ordinal, m.v.Patch))
	return err == nil && c >= 0
}
