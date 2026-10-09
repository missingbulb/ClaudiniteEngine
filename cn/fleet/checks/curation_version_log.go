package checks

import (
	"fmt"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/builtin"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// A shelf pack's provenance/VERSIONS.md rows run newest-first. The history
// task sorts what it writes, but a row a person adds lands wherever they
// put it, and once the tail drifts a number near the bottom could be old or
// merely misplaced. World scope: ordering is a property of the whole file,
// which no one diff sees.
var versionLog = declared.Builtin{
	ID:     "pack-version-log-ordered",
	Pack:   packset.FleetPack,
	OnFail: "block",
	Tags:   []string{"world", "builtin", packset.FleetPack},
	Doc:    "fleet/pack-version-history",
	Why:    "a reader trusts a VERSIONS.md row's position to say its age; once the tail drifts out of sequence a number near the bottom could be old or merely misplaced, and nothing short of re-deriving the order from the numbers themselves can tell which",
}

func init() { builtin.Register(&versionLog, runVersionLog) }

var (
	versionRow = regexp.MustCompile(`^\|\s*(\d+(?:\.\d+)*)\s*\|`)
	dayOrdinal = regexp.MustCompile(`^([1-9]\d{4,5}\.[1-9]\d*|\d+)$`)
)

// rowVersion is a VERSIONS.md line's version: a <major>.<day>.<n>, or an
// earlier date-anchored <day>.<n> or bare integer, which ComparePack sorts
// below it. Any other number in the first column is not a version.
func rowVersion(line string) (string, bool) {
	m := versionRow.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", false
	}
	if dayOrdinal.MatchString(m[1]) {
		return m[1], true
	}
	if v, err := version.Parse(m[1]); err == nil && v != (version.V{}) {
		return m[1], true
	}
	return "", false
}

func runVersionLog(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	type claim struct {
		text string
		line int
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
			if v, ok := rowVersion(l); ok {
				claims = append(claims, claim{v, i + 1})
			}
		}
		for i := 1; i < len(claims); i++ {
			prev, cur := claims[i-1], claims[i]
			if c, _ := version.ComparePack(cur.text, prev.text); c > 0 {
				out = append(out, versionLog.Finding(f, cur.line,
					fmt.Sprintf("version %s sits below %s (line %d) but is newer — VERSIONS.md rows must run newest-first", cur.text, prev.text, prev.line),
					fmt.Sprintf("move the row for %s above line %d, so rows descend by version top to bottom", cur.text, prev.line)))
			}
		}
	}
	return out
}
