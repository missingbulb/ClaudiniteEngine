package usage

import (
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsregex"
)

// logNameRE is the capture filename standard: a minute stamp, an optional
// collision suffix, the PR or issue the capture is keyed to, the session.
var logNameRE = jsPattern(`^(\d{4}-\d{2}-\d{2})T(\d{2})(\d{2})Z(?:-\d+)?--(pr|issue)-(\d+)--(.+)\.jsonl$`)

// LogName is what a capture file's name says.
type LogName struct {
	Date, Stamp string
	Issue, PR   *float64
	SessionID   string
}

// ParseLogName reads a capture file's name, false for any other file.
func ParseLogName(name string) (LogName, bool) {
	m := logNameRE.FindStringSubmatch(name)
	if m == nil {
		return LogName{}, false
	}
	n := stringToNumber(m[5])
	out := LogName{Date: m[1], Stamp: m[1] + "T" + m[2] + ":" + m[3] + ":00Z", SessionID: m[6]}
	if m[4] == "issue" {
		out.Issue = &n
	} else {
		out.PR = &n
	}
	return out, true
}

// ParseEntries is a capture file's entries, a line that does not parse
// skipped.
func ParseEntries(text string) []any {
	out := []any{}
	for _, line := range strings.Split(text, "\n") {
		if jsregex.Trim(line) == "" {
			continue
		}
		if v, err := ParseJSON(line); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// commitMark opens each commit's block in the log CommitSeries reads.
const commitMark = "\u0001"

var numstatRE = jsPattern(`^(\d+|-)\t(\d+|-)\t`)

// ParseCommitLog is the commits and lines per day out of a
// `--pretty=format:%x01%cI --numstat` log.
func ParseCommitLog(text string) *Obj {
	days := NewObj()
	date := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, commitMark) {
			date = sliceUnits(line, 1, 11)
			if date != "" {
				row := days.ObjAt(date)
				if row == nil {
					row = ObjOf("commits", 0.0, "linesAdded", 0.0, "linesRemoved", 0.0)
					days.Set(date, row)
				}
				bump(row, "commits", 1)
			}
			continue
		}
		if date == "" || jsregex.Trim(line) == "" {
			continue
		}
		m := numstatRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		row := days.ObjAt(date)
		if m[1] != "-" {
			bump(row, "linesAdded", stringToNumber(m[1]))
		}
		if m[2] != "-" {
			bump(row, "linesRemoved", stringToNumber(m[2]))
		}
	}
	return days
}

// History is the local git the session half reads: Local runs a command in
// the checkout and errs on a non-zero exit, Remote does the same through
// the engine's git, which carries the job's token.
type History struct {
	Local  func(args ...string) (string, error)
	Remote func(args ...string) (string, error)
}

// DeepenHistory fetches the window's history into a shallow checkout and
// answers which path ran: complete, deepened or unchanged.
func DeepenHistory(h History, base, since string) string {
	shallow, err := h.Local("rev-parse", "--is-shallow-repository")
	if err != nil {
		return "unchanged"
	}
	if strings.TrimSpace(shallow) != "true" {
		return "complete"
	}
	if _, err := h.Remote("fetch", "--quiet", "--shallow-since="+since, "origin", base); err != nil {
		return "unchanged"
	}
	return "deepened"
}

// CommitSeries is the commits and lines per day since an instant, and the
// day the available history starts from.
type CommitSeries struct {
	Days        *Obj
	CoveredFrom string
}

// ReadCommitSeries reads the series off the local history at sha; nil when
// no history could be read.
func ReadCommitSeries(h History, sha, since string) *CommitSeries {
	text, err := h.Local("log", sha, "--since="+since, "--date=iso-strict", "--pretty=format:%x01%cI", "--numstat")
	if err != nil {
		return nil
	}
	out := &CommitSeries{Days: ParseCommitLog(text), CoveredFrom: sliceUnits(since, 0, 10)}
	if shallow, err := h.Local("rev-parse", "--is-shallow-repository"); err == nil && strings.TrimSpace(shallow) == "true" {
		if log, err := h.Local("log", sha, "--reverse", "--pretty=format:%cI"); err == nil {
			oldest := sliceUnits(strings.Split(log, "\n")[0], 0, 10)
			if oldest != "" && jsLess(out.CoveredFrom, oldest) {
				out.CoveredFrom = oldest
			}
		}
	}
	return out
}

// ReleaseSeries is the releases published per day, and whether the one
// page read may have cut the far end off.
type ReleaseSeries struct {
	Days      *Obj
	Truncated bool
}

// ReadReleaseSeries reads the releases listing; nil when it could not be
// read.
func ReadReleaseSeries(r Reader, repo string) (*ReleaseSeries, error) {
	json, err := r.JSON("/repos/" + repo + "/releases?per_page=100")
	if err != nil {
		return nil, err
	}
	list, ok := arrayOf(json)
	if !ok {
		return nil, nil
	}
	days := NewObj()
	for _, rel := range list {
		at := nullish(propOf(rel, "published_at"))
		if at == nil {
			at = nullish(propOf(rel, "created_at"))
		}
		if !truthy(at) {
			continue
		}
		bumpOr(days, sliceUnits(jsString(at), 0, 10), 1.0)
	}
	return &ReleaseSeries{Days: days, Truncated: len(list) >= 100}, nil
}

// DayFieldsFrom merges the two series into the per-day fields the fold
// takes: a day gets a key only from a source that could speak for it.
func DayFieldsFrom(commits *CommitSeries, releases *ReleaseSeries, ladder []string) *Obj {
	out := NewObj()
	for _, date := range ladder {
		fields := NewObj()
		if commits != nil && !jsLess(date, commits.CoveredFrom) {
			row := commits.Days.ObjAt(date)
			if row == nil {
				row = ObjOf("commits", 0.0, "linesAdded", 0.0, "linesRemoved", 0.0)
			}
			for _, k := range row.Keys() {
				v, _ := row.Get(k)
				fields.Set(k, v)
			}
		}
		if releases != nil {
			v, ok := releases.Days.Get(date)
			fields.Set("releases", orZero(v, ok && v != nil))
		}
		if fields.Len() > 0 {
			out.Set(date, fields)
		}
	}
	return out
}

// DayLadder is the window's UTC days ending today, oldest first.
func DayLadder(now string, days int) []string {
	end := parseDate(sliceUnits(now, 0, 10) + "T00:00:00Z")
	out := make([]string, days)
	for i := range out {
		out[i] = sliceUnits(isoString(end-float64(days-1-i)*86400000), 0, 10)
	}
	return out
}
