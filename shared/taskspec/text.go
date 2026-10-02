package taskspec

import (
	"regexp"
	"strings"
)

var (
	termNameLine   = regexp.MustCompile(`(?m)^ {2}['"]([^'"]+)['"]:`)
	needsItemTrue  = regexp.MustCompile(`\bneedsItem\s*:\s*true\b`)
	takesArgTrue   = regexp.MustCompile(`\btakesArg\s*:\s*true\b`)
	signalsLiteral = regexp.MustCompile(`\bsignals\s*:\s*\[([^\]]*)\]`)
	quotedWord     = regexp.MustCompile(`['"]([^'"]+)['"]`)
)

// TermsFromText is the vocabulary a task folder's preconditions.mjs
// exports, read as text with its comments already stripped, never
// imported: each term is the key that defines it, the two properties a
// declaration can be wrong about, and the signals its literal list names.
// A term the file computes rather than spells is not found.
func TermsFromText(body string) Terms {
	start := strings.Index(body, "export const terms")
	if start < 0 {
		return nil
	}
	section := body[start:]
	named := termNameLine.FindAllStringSubmatchIndex(section, -1)
	var out Terms
	at := map[string]int{}
	for i, m := range named {
		end := len(section)
		if i+1 < len(named) {
			end = named[i+1][0]
		}
		block := section[m[0]:end]
		t := TermSpec{
			Name:      section[m[2]:m[3]],
			Signals:   []string{},
			NeedsItem: needsItemTrue.MatchString(block),
			TakesArg:  takesArgTrue.MatchString(block),
		}
		if s := signalsLiteral.FindStringSubmatch(block); s != nil {
			for _, q := range quotedWord.FindAllStringSubmatch(s[1], -1) {
				t.Signals = append(t.Signals, q[1])
			}
		}
		if j, seen := at[t.Name]; seen {
			out[j] = t
			continue
		}
		at[t.Name] = len(out)
		out = append(out, t)
	}
	return out
}

// CadenceTermFor is the condition a retired frequency becomes, "" for
// manual, which meant no schedule at all.
func CadenceTermFor(frequency string) string { return cadenceTermFor(frequency) }

// EscapesTaskDir reports whether a work command names an absolute path or
// climbs out of the task directory.
func EscapesTaskDir(cmd string) bool { return escapesTaskDir(cmd) }

// HasSpace reports whether s holds JavaScript whitespace.
func HasSpace(s string) bool { return hasSpace(s) }
