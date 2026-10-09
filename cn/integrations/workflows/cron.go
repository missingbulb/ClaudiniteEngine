package workflows

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The scheduler's minute band clears GitHub's :00 stampede and the hour
// boundary the anchor arithmetic works from; the anchor hour is one of the
// first twelve, the drain tick twelve hours later.
const (
	minuteMin   = 10
	minuteBand  = 41
	anchorHours = 12
)

// fnv1a is the 32-bit FNV-1a hash of the lower-cased name, as the Node
// engine computes it.
func fnv1a(name string) uint32 {
	h := uint32(0x811c9dc5)
	for _, c := range strings.ToLower(name) {
		h ^= uint32(c)
		h *= 0x01000193
	}
	return h
}

// SchedulerCron is the cron a repo's scheduler runs on, minute and hours
// both derived from its "owner/name": stable, and recomputable anywhere the
// name is known.
func SchedulerCron(fullName string) string {
	h := fnv1a(fullName)
	anchor := (h >> 8) % anchorHours
	return fmt.Sprintf("%d %d,%d * * *", minuteMin+h%minuteBand, anchor, anchor+anchorHours)
}

// ForRepo is the templates as cn init writes them for fullName: the
// scheduler's placeholder cron rewritten to the repo's own.
func ForRepo(fullName string) map[string][]byte {
	out := Templates()
	s := "claudinite-scheduler.yml"
	out[s] = []byte(strings.Replace(string(out[s]), `cron: "`+CronPlaceholder+`"`, `cron: "`+SchedulerCron(fullName)+`"`, 1))
	return out
}

var (
	cronLine    = regexp.MustCompile(`(?m)^    - cron: "([^"\n]*)"$`)
	cronField   = regexp.MustCompile(`^[0-9*/,-]+$`)
	stampedLine = regexp.MustCompile(`^ {10}([A-Z][A-Z0-9_]*): \$\{\{ secrets\.([A-Z][A-Z0-9_]*) \}\}$`)
	secretWord  = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// memberCron is the cron the scheduler have runs on, when it carries one
// GitHub can read: five fields of numbers, steps, lists, ranges and
// stars. A single daily tick is as much the member's choice as the
// engine's hashed pair; "" when there is none.
func memberCron(have []byte) string {
	m := cronLine.FindStringSubmatch(string(have))
	if m == nil {
		return ""
	}
	fields := strings.Fields(m[1])
	if len(fields) != 5 || strings.Join(fields, " ") != m[1] {
		return ""
	}
	for _, f := range fields {
		if !cronField.MatchString(f) {
			return ""
		}
	}
	return m[1]
}

// ErrNoName is a scheduler with no cron of its own, asked for without the
// repo's owner/name: its cron is that name's hash, which nothing else can
// stand in for.
var ErrNoName = errors.New("the scheduler carries no cron of its own, and this repo's owner/name is unknown: pass --name OWNER/NAME")

// Expected is the template name as the member fullName (owner/name), holding
// have, should carry it: the scheduler keeps the member's own cron
// (memberCron) and otherwise takes fullName's hash, never the template's
// placeholder, and the executor keeps the secret lines stamped beneath its
// marker. Anything else is the template's. fullName may be empty while the
// member carries a cron; otherwise Expected fails with ErrNoName.
func Expected(name string, have []byte, fullName string) ([]byte, error) {
	want := string(Templates()[name])
	switch name {
	case "claudinite-scheduler.yml":
		cron := memberCron(have)
		if cron == "" {
			if fullName == "" {
				return nil, ErrNoName
			}
			cron = SchedulerCron(fullName)
		}
		want = strings.Replace(want, `cron: "`+CronPlaceholder+`"`, `cron: "`+cron+`"`, 1)
	case "claudinite-executor.yml":
		var stamped []string
		lines := strings.Split(string(have), "\n")
		for i, l := range lines {
			if strings.TrimSpace(l) != SecretsMarker {
				continue
			}
			for _, s := range lines[i+1:] {
				m := stampedLine.FindStringSubmatch(s)
				if m == nil || m[1] != m[2] {
					break
				}
				stamped = append(stamped, s+"\n")
			}
			break
		}
		if len(stamped) > 0 {
			want = strings.Replace(want, SecretsMarker+"\n", SecretsMarker+"\n"+strings.Join(stamped, ""), 1)
			// Re-stamping drops a name the template already passes.
			if out, ok := Stamp([]byte(want), StampedSecrets([]byte(want))); ok {
				want = string(out)
			}
		}
	}
	return []byte(want), nil
}
