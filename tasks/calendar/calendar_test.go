package calendar

import (
	"testing"
	"time"
)

func TestPeriodStart(t *testing.T) {
	// A Wednesday afternoon, in a zone that is not UTC.
	now := time.Date(2026, 9, 16, 23, 30, 0, 0, time.FixedZone("x", -3*3600))
	for cadence, want := range map[string]string{
		"daily":   "2026-09-17T00:00:00.000Z",
		"weekly":  "2026-09-13T00:00:00.000Z",
		"monthly": "2026-09-01T00:00:00.000Z",
	} {
		got, ok, err := PeriodStart(cadence, now)
		if err != nil || !ok || ISO(got) != want {
			t.Errorf("%s: %s %v %v, want %s", cadence, ISO(got), ok, err, want)
		}
	}
	if _, ok, err := PeriodStart("manual", now); ok || err != nil {
		t.Errorf("manual has a period: %v %v", ok, err)
	}
	if _, _, err := PeriodStart("hourly", now); err == nil {
		t.Error("an unknown cadence is not an error")
	}
	// A period opening exactly now is the current one.
	sunday := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	if got, _, _ := PeriodStart("weekly", sunday); !got.Equal(sunday) {
		t.Errorf("weekly at its opening: %s", ISO(got))
	}
}

func TestPeriod(t *testing.T) {
	for cadence, want := range map[string]time.Duration{"daily": 24 * time.Hour, "weekly": 7 * 24 * time.Hour, "monthly": 31 * 24 * time.Hour} {
		if got, ok := Period(cadence); !ok || got != want {
			t.Errorf("%s: %v %v", cadence, got, ok)
		}
	}
	if _, ok := Period("manual"); ok {
		t.Error("manual has a length")
	}
}
