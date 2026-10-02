package workflows

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// context is the unchanged lines a hunk shows on each side of a change.
const context = 3

// maxLines bounds a file Diff compares; its table is quadratic.
const maxLines = 4000

// Diff is the unified diff, one section per file and empty when nothing
// differs, that turns repo's .github/workflows/ copies into this binary's
// templates; `patch -p1` or `git apply` at the repo root applies it.
func Diff(repo string) (string, error) {
	var b strings.Builder
	for _, name := range Names {
		rel := ".github/workflows/" + name
		have, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		from := "a/" + rel
		if errors.Is(err, os.ErrNotExist) {
			have, from = nil, "/dev/null"
		} else if err != nil {
			return "", err
		}
		d, err := unified(splitLines(have), splitLines(Expected(name, have)))
		if err != nil {
			return "", fmt.Errorf("%s: %w", rel, err)
		}
		if d != "" {
			fmt.Fprintf(&b, "--- %s\n+++ b/%s\n%s", from, rel, d)
		}
	}
	return b.String(), nil
}

// splitLines keeps each line's newline, so a last line without one stays
// distinct from the same text with one.
func splitLines(b []byte) []string {
	var out []string
	s := string(b)
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

type op struct {
	kind byte // ' ', '-' or '+'
	line string
}

// edits is a shortest edit script from a to b by longest common
// subsequence.
func edits(a, b []string) []op {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []op
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			out = append(out, op{' ', a[i]})
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			out = append(out, op{'+', b[j]})
			j++
		default:
			out = append(out, op{'-', a[i]})
			i++
		}
	}
	return out
}

func span(start, n int) string {
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}

func unified(a, b []string) (string, error) {
	if len(a) > maxLines || len(b) > maxLines {
		return "", fmt.Errorf("longer than %d lines", maxLines)
	}
	ops := edits(a, b)
	var b2 strings.Builder
	for k := 0; k < len(ops); {
		if ops[k].kind == ' ' {
			k++
			continue
		}
		start := max(0, k-context)
		end := k
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*context {
				end = min(end+context, run)
				break
			}
			end = run
		}
		aStart, bStart := 0, 0
		for _, o := range ops[:start] {
			if o.kind != '+' {
				aStart++
			}
			if o.kind != '-' {
				bStart++
			}
		}
		aLen, bLen := 0, 0
		var body strings.Builder
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				aLen++
			}
			if o.kind != '-' {
				bLen++
			}
			body.WriteByte(o.kind)
			body.WriteString(o.line)
			if !strings.HasSuffix(o.line, "\n") {
				body.WriteString("\n\\ No newline at end of file\n")
			}
		}
		fmt.Fprintf(&b2, "@@ -%s +%s @@\n%s", span(aStart, aLen), span(bStart, bLen), body.String())
		k = end
	}
	return b2.String(), nil
}
