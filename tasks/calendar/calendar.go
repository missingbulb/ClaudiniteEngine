// Package calendar is the scheduling calendar: given a cadence and an
// instant, when did that cadence's current period open. The period is the
// UTC calendar and nothing a repo configures: a day opens at midnight UTC,
// a week on the Sunday that opened it, a month on its 1st. It never reads
// a clock; the instant is always passed in.
package calendar

import (
	"fmt"
	"time"
)

const day = 24 * time.Hour

// PeriodStart is when cadence's current period opened at now; a period
// opening exactly at now is the current one. Manual has no period.
func PeriodStart(cadence string, now time.Time) (time.Time, bool, error) {
	at := now.UTC()
	midnight := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	switch cadence {
	case "manual":
		return time.Time{}, false, nil
	case "daily":
		return midnight, true, nil
	case "weekly":
		return midnight.Add(-time.Duration(int(at.Weekday())) * day), true, nil
	case "monthly":
		return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC), true, nil
	}
	return time.Time{}, false, fmt.Errorf("unknown frequency %q", cadence)
}

// Period is a cadence's nominal length: a week, 31 days for a month (the
// longest, so a window never falls short of one), a day otherwise; manual
// has none.
func Period(cadence string) (time.Duration, bool) {
	switch cadence {
	case "weekly":
		return 7 * day, true
	case "monthly":
		return 31 * day, true
	case "manual":
		return 0, false
	}
	return day, true
}

// NextAnchor is when cadence's next period opens, strictly after now,
// walked forward from PeriodStart since months are not a fixed distance
// apart.
func NextAnchor(cadence string, now time.Time) (time.Time, bool) {
	start, ok, err := PeriodStart(cadence, now)
	if !ok || err != nil {
		return time.Time{}, false
	}
	step, _ := Period(cadence)
	if cadence == "monthly" {
		step = 28 * day
	}
	for t := start.Add(step); ; t = t.Add(step) {
		if c, _, _ := PeriodStart(cadence, t); c.After(start) {
			return c, true
		}
	}
}

// ISO is JavaScript's Date#toISOString: UTC, milliseconds, a Z.
func ISO(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// ParseInstant reads an instant as JavaScript's Date does for the forms
// the queue writes (an ISO date-time, a bare ISO date as UTC midnight); a
// date-time without an offset reads as UTC, the runner's zone.
func ParseInstant(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04Z07:00", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
