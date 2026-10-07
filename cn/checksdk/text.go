package checksdk

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode/utf16"
)

// CharsPerToken is the measured ratio a token estimate divides by.
const CharsPerToken = 4.2

// CountChars is text's length as JavaScript's String.length counts it,
// in UTF-16 units, which the token ratio was measured in.
func CountChars(text string) int { return len(utf16.Encode([]rune(text))) }

// EstimateTokens is how many tokens chars characters cost a session,
// rounded half up.
func EstimateTokens(chars int) int { return int(math.Floor(float64(chars)/CharsPerToken + 0.5)) }

var workflowRe = regexp.MustCompile(`^\.github/workflows/[^/]+\.ya?ml$`)

// WorkflowFiles are the tracked GitHub Actions workflow files.
func WorkflowFiles(r Repo) []string {
	var out []string
	for _, f := range r.Tracked() {
		if workflowRe.MatchString(f) {
			out = append(out, f)
		}
	}
	return out
}

// MatchingLines are the lines of files matching re, in order.
func MatchingLines(r Repo, files []string, re *regexp.Regexp) []Line {
	var out []Line
	for _, f := range files {
		text, ok := r.Read(f)
		if !ok {
			continue
		}
		for i, l := range strings.Split(text, "\n") {
			if re.MatchString(l) {
				out = append(out, Line{Path: f, Line: i + 1, Text: l})
			}
		}
	}
	return out
}

func parseJSON(text string, ok bool) any {
	if !ok {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(text), &v) != nil {
		return nil
	}
	return v
}

// JSONPair is file parsed as JSON in the working tree and at the merge
// base; a side that is absent or does not parse is nil.
func JSONPair(r Repo, file string) (head, base any) {
	return parseJSON(r.Read(file)), parseJSON(r.ReadBase(file))
}

// FilesContaining are the files (nil: every scanned file) whose text
// contains needle.
func FilesContaining(r Repo, needle string, files []string) []string {
	if files == nil {
		files = r.Files()
	}
	var out []string
	for _, f := range files {
		if t, _ := r.Read(f); strings.Contains(t, needle) {
			out = append(out, f)
		}
	}
	return out
}
