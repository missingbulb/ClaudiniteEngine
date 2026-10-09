// Package proc is the one place cn starts a process. Every start is
// counted under its kind, so a test can hold a command to its budget of
// processes, and with CN_PROC_LOG naming a file each start is appended
// there as one line, children of children included, for a trace across
// processes.
//
// A kind is the program's base name, and for git its subcommand too
// ("git show"): the processes cn may legitimately start are pack code, the
// Go toolchain, the checks binary, another engine version and git's
// network transfers; local git reads are not among them.
package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// LogEnv names the file every start is appended to.
const LogEnv = "CN_PROC_LOG"

var (
	mu     sync.Mutex
	counts = map[string]int{}
)

// Command is exec.Command, counted.
func Command(name string, args ...string) *exec.Cmd {
	note(name, args)
	return exec.Command(name, args...)
}

// CommandContext is exec.CommandContext, counted.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	note(name, args)
	return exec.CommandContext(ctx, name, args...)
}

// Kind is the kind a start of name with args counts under.
func Kind(name string, args []string) string {
	base := strings.TrimSuffix(filepath.Base(name), ".exe")
	if base != "git" {
		return base
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-c" || a == "-C" || a == "--git-dir" || a == "--work-tree":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return "git " + a
		}
	}
	return "git"
}

func note(name string, args []string) {
	kind := Kind(name, args)
	mu.Lock()
	counts[kind]++
	mu.Unlock()
	if p := os.Getenv(LogEnv); p != "" {
		if f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600); err == nil {
			_, _ = f.WriteString(kind + "\t" + strings.Join(append([]string{name}, args...), " ") + "\n")
			_ = f.Close()
		}
	}
}

// Counts is the starts so far by kind.
func Counts() map[string]int {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]int, len(counts))
	for k, v := range counts {
		out[k] = v
	}
	return out
}

// Reset forgets the starts counted so far.
func Reset() {
	mu.Lock()
	counts = map[string]int{}
	mu.Unlock()
}

// Total is the number of starts in c.
func Total(c map[string]int) int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

// Format renders c as "kind×n" words in kind order.
func Format(c map[string]int) string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k)
		b.WriteString("×")
		b.WriteString(strconv.Itoa(c[k]))
	}
	return b.String()
}
