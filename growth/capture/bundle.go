package capture

import (
	"encoding/json"
	"sort"
	"strings"
)

// Line is one transcript line: its raw bytes, which are what a capture
// writes, and the timestamp it carries ("" for none), the only field a
// capture reads. A re-encoding could reorder keys, so a line is never
// decoded into anything but its timestamp.
type Line struct {
	Raw string
	// Value is the line as JSON, for the parity cores.
	Value json.RawMessage
	TS    string
	// HasTS is set when the line carries a string timestamp.
	HasTS bool
}

// ParseLines splits a transcript into lines, skipping blank ones and any
// that is not JSON (a partial trailing write).
func ParseLines(text string) []Line {
	var out []Line
	for _, raw := range strings.Split(text, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if !json.Valid([]byte(raw)) {
			continue
		}
		l := Line{Raw: raw, Value: json.RawMessage(strings.TrimSpace(raw))}
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &obj) == nil {
			if ts, ok := obj["timestamp"]; ok {
				var s string
				if json.Unmarshal(ts, &s) == nil {
					l.TS, l.HasTS = s, true
				}
			}
		}
		out = append(out, l)
	}
	return out
}

// Bundled is one line placed in a capture: its raw bytes and the
// timestamp it sorts by, a timestampless line's inherited from its
// stream predecessor.
type Bundled struct {
	Raw string
	TS  string
}

// Bundle merges the main transcript with its sidechain streams into one
// timestamp-ordered list. A timestampless line inherits its stream
// predecessor's timestamp, so it stays beside its neighbours; ties keep
// stream order, then line order.
func Bundle(streams [][]Line) []Bundled {
	type tagged struct {
		b    Bundled
		s, i int
	}
	var all []tagged
	for s, lines := range streams {
		carry := ""
		for i, l := range lines {
			if l.HasTS {
				carry = l.TS
			}
			all = append(all, tagged{Bundled{l.Raw, carry}, s, i})
		}
	}
	sort.SliceStable(all, func(a, b int) bool {
		x, y := all[a], all[b]
		if x.b.TS != y.b.TS {
			return x.b.TS < y.b.TS
		}
		if x.s != y.s {
			return x.s < y.s
		}
		return x.i < y.i
	})
	out := make([]Bundled, len(all))
	for i, t := range all {
		out[i] = t.b
	}
	return out
}

// SliceAfter is the lines strictly after lastTs, all of them for "".
func SliceAfter(bundled []Bundled, lastTs string) []Bundled {
	if lastTs == "" {
		return bundled
	}
	var out []Bundled
	for _, l := range bundled {
		if l.TS > lastTs {
			out = append(out, l)
		}
	}
	return out
}

// MaxTimestamp is the latest timestamp bundled carries, "" for none.
func MaxTimestamp(bundled []Bundled) string {
	max := ""
	for _, l := range bundled {
		if l.TS != "" && l.TS > max {
			max = l.TS
		}
	}
	return max
}
