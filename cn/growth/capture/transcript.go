package capture

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// ProjectsRoot is where Claude Code keeps session transcripts:
// <CLAUDE_CONFIG_DIR or ~/.claude>/projects.
func ProjectsRoot(getenv func(string) string, home string) string {
	return filepath.Join(ConfigDir(getenv, home), "projects")
}

// ConfigDir is Claude Code's config root: CLAUDE_CONFIG_DIR when set,
// else ~/.claude.
func ConfigDir(getenv func(string) string, home string) string {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home, ".claude")
}

// Slug is the projects directory Claude Code names for a launch path:
// every character but an ASCII letter or digit is a dash, one per UTF-16
// unit.
func Slug(root string) string {
	var b strings.Builder
	for _, r := range root {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		n := utf16.RuneLen(r)
		if n < 1 {
			n = 1
		}
		b.WriteString(strings.Repeat("-", n))
	}
	return b.String()
}

func modTime(p string) (time.Time, bool) {
	st, err := os.Stat(p)
	if err != nil {
		return time.Time{}, false
	}
	return st.ModTime(), true
}

// newestTranscript is dir's .jsonl with the newest modification time,
// the first in name order on a tie; "" for none.
func newestTranscript(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, bestAt := "", time.Time{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		at, ok := modTime(p)
		if !ok {
			continue
		}
		if best == "" || at.After(bestAt) {
			best, bestAt = p, at
		}
	}
	return best
}

// FindTranscript locates a session's transcript: the session id names
// the file exactly, so every project directory is searched for it first,
// which survives a launch path the slug never reproduces; then the slug
// directory's newest transcript; then the newest anywhere. "" for none.
func FindTranscript(root, sessionID, projects string) string {
	var dirs []string
	if entries, err := os.ReadDir(projects); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(projects, e.Name()))
			}
		}
	}
	if sessionID != "" {
		for _, d := range dirs {
			hit := filepath.Join(d, sessionID+".jsonl")
			if _, err := os.Stat(hit); err == nil {
				return hit
			}
		}
	}
	if p := newestTranscript(filepath.Join(projects, Slug(root))); p != "" {
		return p
	}
	best, bestAt := "", time.Time{}
	for _, d := range dirs {
		p := newestTranscript(d)
		if p == "" {
			continue
		}
		if at, ok := modTime(p); ok && (best == "" || at.After(bestAt)) {
			best, bestAt = p, at
		}
	}
	return best
}

var agentFile = regexp.MustCompile(`^agent-.*\.jsonl$`)

// Sidechains are the subagent streams under the session's own directory,
// swept at any depth: an older harness's layout still captures whole.
func Sidechains(transcript, sessionID string) []string {
	stack := []string{filepath.Join(filepath.Dir(transcript), sessionID)}
	var out []string
	for len(stack) > 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			switch {
			case e.IsDir():
				stack = append(stack, filepath.Join(dir, e.Name()))
			case agentFile.MatchString(e.Name()):
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}
