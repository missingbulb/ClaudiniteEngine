package workflows

import (
	"sort"
	"strings"
)

// StampedSecrets are the secret names stamped beneath the executor's
// marker line, in file order.
func StampedSecrets(have []byte) []string {
	lines := strings.Split(string(have), "\n")
	start, end := stampedBlock(lines)
	var out []string
	if start < 0 {
		return nil
	}
	for _, l := range lines[start:end] {
		out = append(out, stampedLine.FindStringSubmatch(l)[1])
	}
	return out
}

// Stamp writes one env line per secret beneath the executor's marker,
// sorted, in place of the lines stamped there; a name the file already
// passes elsewhere in that env is left to that line. ok is false when the
// file holds no marker line, and executor comes back unchanged.
func Stamp(executor []byte, secrets []string) (out []byte, ok bool) {
	lines := strings.Split(string(executor), "\n")
	start, end := stampedBlock(lines)
	if start < 0 {
		return executor, false
	}
	outside := map[string]bool{}
	for i, l := range lines {
		if i >= start && i < end {
			continue
		}
		if m := stampedLine.FindStringSubmatch(l); m != nil && m[1] == m[2] {
			outside[m[1]] = true
		}
	}
	seen := map[string]bool{}
	var names []string
	for _, s := range secrets {
		if !seen[s] && !outside[s] && secretWord.MatchString(s) {
			seen[s] = true
			names = append(names, s)
		}
	}
	sort.Strings(names)
	var block []string
	for _, n := range names {
		block = append(block, "          "+n+": ${{ secrets."+n+" }}")
	}
	next := append(append(append([]string{}, lines[:start]...), block...), lines[end:]...)
	return []byte(strings.Join(next, "\n")), true
}

// stampedBlock is the half-open line range of the stamped lines beneath
// the marker; start is -1 when there is no marker.
func stampedBlock(lines []string) (int, int) {
	for i, l := range lines {
		if strings.TrimSpace(l) != SecretsMarker {
			continue
		}
		end := i + 1
		for end < len(lines) {
			m := stampedLine.FindStringSubmatch(lines[end])
			if m == nil || m[1] != m[2] {
				break
			}
			end++
		}
		return i + 1, end
	}
	return -1, -1
}
