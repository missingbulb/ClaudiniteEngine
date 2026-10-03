package builtin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

// A shelf pack's provenance/VERSIONS.md rows run newest-first. The history
// task sorts what it writes, but a row a person adds lands wherever they
// put it, and once the tail drifts a number near the bottom could be old or
// merely misplaced. World scope: ordering is a property of the whole file,
// which no one diff sees.
var versionLog = declared.Builtin{
	ID:     "pack-version-log-ordered",
	Pack:   "claudinite-canon-curation",
	OnFail: "block",
	Tags:   []string{"world", "builtin", "claudinite-canon-curation"},
	Doc:    "packs/claudinite-canon-curation/README.md",
	Why:    "a reader trusts a VERSIONS.md row's position to say its age; once the tail drifts out of sequence a number near the bottom could be old or merely misplaced, and nothing short of re-deriving the order from the numbers themselves can tell which",
}

func init() { register(&versionLog, runVersionLog) }

var (
	versionRow = regexp.MustCompile(`^\|\s*(\d+(?:\.\d+)?)\s*\|`)
	dayOrdinal = regexp.MustCompile(`^([1-9]\d{4,5})\.([1-9]\d*)$`)
)

// rowVersion is a VERSIONS.md line's version as <day> and <n>: a
// date-anchored <day>.<n>, or a bare integer read as <day>.0.
func rowVersion(line string) (day, n int, ok bool) {
	m := versionRow.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return 0, 0, false
	}
	if !strings.Contains(m[1], ".") {
		d, err := strconv.Atoi(m[1])
		return d, 0, err == nil
	}
	v := dayOrdinal.FindStringSubmatch(m[1])
	if v == nil {
		return 0, 0, false
	}
	day, _ = strconv.Atoi(v[1])
	n, _ = strconv.Atoi(v[2])
	return day, n, true
}

func runVersionLog(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	type claim struct {
		text   string
		day, n int
		line   int
	}
	var out []findings.Finding
	for _, f := range ctx.Files() {
		parts := strings.Split(f, "/")
		if len(parts) != 4 || parts[0] != "packs" || parts[2] != "provenance" || parts[3] != "VERSIONS.md" {
			continue
		}
		text, _ := ctx.Read(f)
		var claims []claim
		for i, l := range strings.Split(text, "\n") {
			if day, n, ok := rowVersion(l); ok {
				claims = append(claims, claim{versionRow.FindStringSubmatch(strings.TrimSpace(l))[1], day, n, i + 1})
			}
		}
		for i := 1; i < len(claims); i++ {
			prev, cur := claims[i-1], claims[i]
			if cur.day > prev.day || (cur.day == prev.day && cur.n > prev.n) {
				out = append(out, versionLog.Finding(f, cur.line,
					fmt.Sprintf("version %s sits below %s (line %d) but is newer — VERSIONS.md rows must run newest-first", cur.text, prev.text, prev.line),
					fmt.Sprintf("move the row for %s above line %d, so rows descend by version top to bottom", cur.text, prev.line)))
			}
		}
	}
	return out
}
