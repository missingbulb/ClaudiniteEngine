package usage

import (
	"regexp"
	"strings"
	"testing"
)

func fileOf(date, session string, es []any) CaptureFile {
	return CaptureFile{Date: date, Issue: fp(0), SessionID: session, Counts: CountEntries(es, Corpus{})}
}

func TestReadCheckBuildReadsTheBuildAndItsWaitsEachRecordedLineOnce(t *testing.T) {
	stop := hookTurn("[cn] buildwait stop ok 4200ms", "[cn] build compiled ok 5712ms", "[cn] checks stop ok 4300ms")
	got := ReadCheckBuild(entries(
		hookTurn("# Claudinite engine 1.61005.1", "[cn] build started ok 3ms", "[cn] hooks session-start ok 40ms"),
		stop, stop,
		hookTurn("[cn] buildwait check timeout 600000ms"),
	))
	same(t, got.Record, `{"cached": 0, "started": 1, "compiledMs": 5712, "compiledOk": 1,
		"waits": {"stop": {"waits": 1, "totalMs": 4200, "maxMs": 4200, "timeouts": 0}, "check": {"waits": 1, "totalMs": 600000, "maxMs": 600000, "timeouts": 1}}}`)
}

func TestReadCheckBuildACachedSessionWaitedNothingAFailedBuildSaysSo(t *testing.T) {
	same(t, ReadCheckBuild(entries(hookTurn("[cn] build cached ok 2ms"))).Record, `{"cached": 1, "started": 0, "waits": {}}`)
	if v := field(ReadCheckBuild(entries(hookTurn("[cn] build compiled error 900ms"))).Record, "compiledOk"); v != 0.0 {
		t.Fatal(v)
	}
	if got := ReadCheckBuild(entries(hookTurn("[cn] checks stop ok 4ms"))); got != nil {
		t.Fatal("an engine that leaves no build line is not a session that built nothing")
	}
}

func TestFoldDaysFilesEachSessionsBuildOnceAndItsWaitsByEvent(t *testing.T) {
	start := hookTurn("[cn] build started ok 3ms")
	stop := hookTurn("[cn] buildwait stop ok 4200ms", "[cn] build compiled ok 5712ms")
	days, err := FoldDays([]CaptureFile{
		fileOf("2026-10-05", "s1", entries(start, stop)),
		fileOf("2026-10-05", "s1", entries(start, stop, hookTurn("[cn] hooks stop ok 3ms"))),
		fileOf("2026-10-05", "s2", entries(hookTurn("[cn] build cached ok 2ms"))),
		fileOf("2026-10-05", "s3", entries(hookTurn("[cn] hooks stop ok 3ms"))),
	})
	if err != nil {
		t.Fatal(err)
	}
	day := days.ObjAt("2026-10-05")
	same(t, field(day, "buildSessions"), `{
		"s1": {"cached": 0, "started": 1, "compiledMs": 5712, "compiledOk": 1, "waits": 1, "waitMs": 4200, "waitMaxMs": 4200},
		"s2": {"cached": 1, "started": 0, "waits": 0, "waitMs": 0, "waitMaxMs": 0}}`)
	same(t, field(day, "buildWaits"), `{"stop": {"waits": 1, "sessions": 1, "totalMs": 4200, "maxMs": 4200, "timeouts": 0}}`)
}

func TestAWeekKeepsEachSessionsBuildOnceAndItsSlowestWaitAsAPeak(t *testing.T) {
	day := func(ms string) *Obj {
		days, err := FoldDays([]CaptureFile{fileOf("2026-09-28", "s1", entries(hookTurn("[cn] buildwait stop ok "+ms+"ms", "[cn] build compiled ok 5000ms")))})
		if err != nil {
			t.Fatal(err)
		}
		return days.ObjAt("2026-09-28")
	}
	first, err := AddDayToWeek(nil, day("100"))
	if err != nil {
		t.Fatal(err)
	}
	week, err := AddDayToWeek(first, day("300"))
	if err != nil {
		t.Fatal(err)
	}
	same(t, field(week, "buildWaits.stop"), `{"waits": 2, "sessions": 2, "totalMs": 400, "maxMs": 300, "timeouts": 0}`)
	if v := field(week, "buildSessions.s1.compiledMs"); v != 5000.0 {
		t.Fatalf("a session spanning two days is one build, not two: %v", v)
	}
	back := DecodeUsage(js(t, Stringify(EncodeUsage(UsageFile{Weeks: ObjOf("2026-W40", week)})))).Weeks.ObjAt("2026-W40")
	if !deepEqual(field(back, "buildSessions"), field(week, "buildSessions")) || !deepEqual(field(back, "buildWaits"), field(week, "buildWaits")) {
		t.Fatalf("%s", Stringify(back))
	}
}

func weekOf(t *testing.T, sessions, waits string) *Obj {
	return spread(ObjOf("days", 7.0), ObjOf("buildSessions", jsObj(t, sessions), "buildWaits", jsObj(t, waits)))
}

func TestTheReportIsTheLastClosedWeekAgainstTheOneBefore(t *testing.T) {
	weeks := ObjOf(
		"2026-W39", weekOf(t, `{"a": {"cached": 1, "started": 0, "waits": 0, "waitMs": 0, "waitMaxMs": 0}}`, `{}`),
		"2026-W40", weekOf(t, `{
			"a": {"cached": 0, "started": 1, "compiledMs": 5700, "compiledOk": 1, "waits": 1, "waitMs": 4200, "waitMaxMs": 4200},
			"b": {"cached": 0, "started": 1, "compiledMs": 200, "compiledOk": 1, "waits": 0, "waitMs": 0, "waitMaxMs": 0},
			"c": {"cached": 0, "started": 1, "compiledMs": 900, "compiledOk": 0, "waits": 0, "waitMs": 0, "waitMaxMs": 0},
			"d": {"cached": 1, "started": 0, "waits": 0, "waitMs": 0, "waitMaxMs": 0}}`,
			`{"stop": {"waits": 1, "sessions": 1, "totalMs": 4200, "maxMs": 4200, "timeouts": 0}}`),
		"2026-W41", weekOf(t, `{"e": {"cached": 1, "started": 0, "waits": 0, "waitMs": 0, "waitMaxMs": 0}}`, `{}`),
	)
	r := CheckBuildReport(weeks, "2026-W41")
	if r.Window != "2026-W40" || r.Previous != "2026-W39" {
		t.Fatalf("%+v", r)
	}
	same(t, r.Current, `{"sessions": 4,
		"builds": {"cached": 1, "compiledOk": 2, "compiledError": 1, "unreported": 0},
		"compiled": {"count": 3, "medianMs": 900, "maxMs": 5700},
		"waited": {"sessions": 1, "totalMs": 4200, "maxMs": 4200},
		"waitsByEvent": {"stop": {"waits": 1, "sessions": 1, "totalMs": 4200, "maxMs": 4200, "timeouts": 0}}}`)
	if r.Prior.Has("compiled") {
		t.Fatal("a week with no compile has no compiled figure")
	}
	same(t, field(r.Prior, "waited"), `{"sessions": 0, "totalMs": 0, "maxMs": 0}`)
}

func TestAWeekNoSessionReportedABuildInIsAbsentAndSoIsAnEmptyReport(t *testing.T) {
	r := CheckBuildReport(ObjOf(
		"2026-W40", weekOf(t, `{"a": {"cached": 1, "started": 0, "waits": 0, "waitMs": 0, "waitMaxMs": 0}}`, `{}`),
		"2026-W39", ObjOf("days", 7.0),
	), "2026-W41")
	if r.Prior != nil {
		t.Fatalf("%s", Stringify(r.Prior))
	}
	if lines := RenderCheckBuildReport(CheckBuildReport(NewObj(), "2026-W41")); len(lines) != 0 {
		t.Fatalf("%v", lines)
	}
	lines := strings.Join(RenderCheckBuildReport(r), "\n")
	if !regexp.MustCompile(`2026-W40`).MatchString(lines) || !strings.Contains(lines, "not recorded") {
		t.Fatal(lines)
	}
}
