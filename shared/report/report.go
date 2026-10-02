package report

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
)

// Code is an error's stable class.
type Code string

const (
	Usage    Code = "usage"
	IO       Code = "io"
	Verify   Code = "verify"
	Internal Code = "internal"
	// Block is a PreToolUse hook denying the call: its reason is already on
	// stderr, and exit 2 is what Claude Code reads as the denial.
	Block Code = "block"
)

// Error is an engine error with its class.
type Error struct {
	Code Code
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// New makes an error of class code.
func New(code Code, msg string) *Error { return &Error{Code: code, Msg: msg} }

// Wrap classes err under code with context msg.
func Wrap(code Code, msg string, err error) *Error { return &Error{Code: code, Msg: msg, Err: err} }

// CodeOf is err's class; an unclassed error is internal.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Internal
}

// Exit prints err as one line, "cn: <code>: <message>", and returns the
// process exit code: 0 for nil, 2 for usage, else 1 for a command and 0 for
// a hook, which must never fail the session it serves. A hook's block
// prints nothing and exits 2.
func Exit(stderr io.Writer, err error, hook bool) int {
	if err == nil {
		return 0
	}
	code := CodeOf(err)
	if code == Block {
		return 2
	}
	msg := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "cn: %s: %s\n", code, msg)
	switch {
	case code == Usage:
		return 2
	case hook:
		return 0
	default:
		return 1
	}
}

// Crash is what a crash file records. It never holds the environment or stdin.
type Crash struct {
	Version  string
	Platform string
	Args     []string
	Value    any
	Stack    []byte
	At       time.Time
}

// CrashDir is the crash folder under the cache root.
func CrashDir(cacheRoot string) string { return filepath.Join(cacheRoot, "crashes") }

// WriteCrash writes <dir>/<utc-timestamp>-<pid>.txt with mode 0600 and
// returns its path, creating dir private if missing.
func WriteCrash(dir string, c Crash) (string, error) {
	if err := paths.EnsurePrivateDir(dir); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%d.txt", c.At.UTC().Format("20060102T150405.000Z"), os.Getpid())
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, werr := fmt.Fprintf(f, "cn crash\nversion: %s\nplatform: %s\ntime: %s\ncommand: cn %s\npanic: %v\n\n%s\n",
		c.Version, c.Platform, c.At.UTC().Format(time.RFC3339Nano), strings.Join(c.Args, " "), c.Value, c.Stack)
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	return path, cerr
}

var crashName = regexp.MustCompile(`-[0-9]+\.txt$`)

const (
	keepFor = 30 * 24 * time.Hour
	keepMax = 50
)

type crashFile struct {
	path  string
	mtime time.Time
}

func listCrashes(dir string) ([]crashFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []crashFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !crashName.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, crashFile{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.Before(out[j].mtime) })
	return out, nil
}

// Prune deletes crash files older than 30 days, then the oldest until at
// most 50 remain. A missing folder is nothing to prune.
func Prune(dir string, now time.Time) error {
	files, err := listCrashes(dir)
	if err != nil {
		return err
	}
	var kept []crashFile
	for _, f := range files {
		if now.Sub(f.mtime) > keepFor {
			_ = os.Remove(f.path)
			continue
		}
		kept = append(kept, f)
	}
	for len(kept) > keepMax {
		_ = os.Remove(kept[0].path)
		kept = kept[1:]
	}
	return nil
}

// CountRecent counts crash files newer than window.
func CountRecent(dir string, now time.Time, window time.Duration) int {
	files, _ := listCrashes(dir)
	n := 0
	for _, f := range files {
		if now.Sub(f.mtime) <= window {
			n++
		}
	}
	return n
}
