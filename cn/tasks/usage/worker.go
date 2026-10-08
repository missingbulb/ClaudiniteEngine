package usage

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LogsBranch is the orphan branch the capture step writes sessions to.
const LogsBranch = "conversation-logs"

// MountedSkills is the skill names the declared packs' trees carry: each
// directory under a pack's skills/ holding a SKILL.md.
func MountedSkills(packDirs []string) map[string]bool {
	out := map[string]bool{}
	for _, dir := range packDirs {
		entries, err := os.ReadDir(filepath.Join(dir, "skills"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if _, err := os.Stat(filepath.Join(dir, "skills", e.Name(), "SKILL.md")); e.IsDir() && err == nil {
				out[e.Name()] = true
			}
		}
	}
	return out
}

// MinuteRateFrom is what a minute of Actions costs, from the tasks pack's
// config; nil where it is unset or not a finite non-negative number.
func MinuteRateFrom(config map[string]any) *float64 {
	rate, ok := config["actionsMinuteRate"].(float64)
	if !ok || math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
		return nil
	}
	return &rate
}

// Fold is one run of the usage fold over a checkout: Root and Repo, the
// Base branch and its tip, the instant the run reads as now, and the
// sources it reads.
type Fold struct {
	Root, Repo, Base, BaseSha string
	Now                       string
	Reader                    Reader
	History                   History
	Mounted                   map[string]bool
	MinuteRate                *float64
	Log                       func(string)
}

// readPrior is a rolling file's prior state, parsed; nil where it is
// absent or does not parse.
func (f Fold) readPrior(path, legacy string) (any, []Move) {
	text, moves := ReadRollingAt(f.History, f.BaseSha, path, legacy)
	raw := "{}"
	if text != nil {
		raw = *text
	}
	v, err := ParseJSON(raw)
	if err != nil {
		return nil, moves
	}
	return v, moves
}

// unchanged reports the base already holding text at path, but for its
// generated stamp.
func (f Fold) unchanged(path, text string) bool {
	landed := ReadAt(f.History, f.BaseSha, path)
	return landed != nil && WithoutStamp(*landed) == WithoutStamp(text)
}

// logFiles fetches the logs branch and answers its tip and capture file
// names; ok is false where the branch does not exist.
func (f Fold) logFiles() (tip string, names []string, ok bool, err error) {
	heads, err := f.History.Remote("ls-remote", "--heads", "origin", LogsBranch)
	if err != nil || strings.TrimSpace(heads) == "" {
		return "", nil, false, err
	}
	if _, err := f.History.Remote("fetch", "--quiet", "origin", LogsBranch); err != nil {
		return "", nil, false, err
	}
	if tip, err = f.History.Local("rev-parse", "FETCH_HEAD"); err != nil {
		return "", nil, false, err
	}
	tip = strings.TrimSpace(tip)
	listed, err := f.History.Local("ls-tree", "--name-only", tip)
	if err != nil {
		return "", nil, false, err
	}
	for _, n := range strings.Split(listed, "\n") {
		if _, isLog := ParseLogName(n); n != "" && isLog {
			names = append(names, n)
		}
	}
	return tip, jsSort(names), true, nil
}

// FoldSessions is the session half: capture files, run listings, closed
// items, merged PRs and the git history, folded into UsagePath.
func (f Fold) FoldSessions() (Folded, error) {
	tip, names, found, err := f.logFiles()
	if err != nil {
		return Folded{}, err
	}
	if !found {
		f.Log("no " + LogsBranch + " branch — nothing captured yet; folding the run, queue and git sources only")
	}
	corpus := Corpus{Mounted: f.Mounted}
	files := []CaptureFile{}
	for _, name := range names {
		text, err := f.History.Local("show", tip+":"+name)
		if err != nil {
			return Folded{}, err
		}
		n, _ := ParseLogName(name)
		files = append(files, CaptureFile{Date: n.Date, Stamp: n.Stamp, Issue: n.Issue, PR: n.PR, SessionID: n.SessionID,
			Counts: CountEntries(ParseEntries(text), corpus)})
	}
	raw, moves := f.readPrior(UsagePath, LegacyUsagePath)
	prior := DecodeUsage(raw)

	runs := ReadRuns(f.Reader, f.Repo, prior.RunsFoldedThrough, f.Now)
	if runs.Error != "" {
		f.Log(runs.Error + " — the hour rows' run counts are unchanged this run")
	}
	queue := ReadQueueOutcomes(f.Reader, f.Repo, prior.QueueFoldedThrough, f.Now)
	if queue.Error != "" {
		f.Log(queue.Error + " — the queue outcome rows are unchanged this run")
	}
	prs := ReadMergedPrs(f.Reader, f.Repo, prior.PrsFoldedThrough, f.Now)
	if prs.Error != "" {
		f.Log(prs.Error + " — the merged-PR rows are unchanged this run")
	}

	ladder := DayLadder(f.Now, DayWindowDays)
	windowStart := ladder[0] + "T00:00:00Z"
	f.Log("git history: " + DeepenHistory(f.History, f.Base, windowStart) + " for the window from " + ladder[0])
	commits := ReadCommitSeries(f.History, f.BaseSha, windowStart)
	if commits == nil {
		f.Log("the local git history could not be read — the commit and line rows are absent this run")
	} else if jsLess(ladder[0], commits.CoveredFrom) {
		f.Log("this checkout's history starts at " + commits.CoveredFrom + " — days before it carry no commit or line counts")
	}
	releases, err := ReadReleaseSeries(f.Reader, f.Repo)
	if err != nil {
		return Folded{}, err
	}
	if releases == nil {
		f.Log("the releases listing could not be read — the release rows are absent this run")
	} else if releases.Truncated {
		f.Log("more than 100 releases exist — the far end of the release series may be under-counted")
	}

	today := sliceUnits(f.Now, 0, 10)
	now := f.Now
	folded, err := FoldUsage(FoldIn{
		Files: files, Prior: prior, Today: today, Now: &now, Generated: now,
		Runs: runs.Runs, RunsFoldedThrough: runs.Watermark,
		QueueRecords: queue.Records, QueueFoldedThrough: queue.Watermark,
		PrRecords: PrRecordsFrom(prs.Prs, files), PrsFoldedThrough: prs.Watermark,
		DayFields: DayFieldsFrom(commits, releases, ladder),
	})
	if err != nil {
		return Folded{}, err
	}
	text := RenderUsageFile(EncodeUsage(folded))
	report := RenderCheckBuildReport(CheckBuildReport(folded.Weeks, IsoWeek(today)))
	summary := strconv.Itoa(len(files)) + " capture file(s), " + strconv.Itoa(len(runs.Runs)) + " run(s), " +
		strconv.Itoa(len(queue.Records)) + " closed item(s) and " + strconv.Itoa(len(prs.Prs)) + " merged PR(s)"
	if f.unchanged(UsagePath, text) {
		return Folded{Summary: summary + " - byte-identical", Report: report}, nil
	}
	return Folded{Files: []File{{UsagePath, text}}, Moves: moves, Summary: summary, Report: report}, nil
}

// FoldMachinery is the machinery half: what the scheduler and executor
// cost and how their items came out, folded into TasksUsagePath.
func (f Fold) FoldMachinery() (Folded, error) {
	if f.MinuteRate == nil {
		f.Log("no `actionsMinuteRate` in this pack's config — the file records minutes and no spend")
	}
	raw, moves := f.readPrior(TasksUsagePath, LegacyTasksUsagePath)
	prior := DecodeTasksUsage(raw)

	runs, err := ReadRunCosts(f.Reader, f.Repo, prior.RunsFoldedThrough, f.Now, MaxRunReads)
	if err != nil {
		return Folded{}, err
	}
	if runs.Error != "" {
		f.Log(runs.Error + " — the run and cost rows are unchanged this run")
	}
	if runs.Truncated {
		f.Log("more runs are waiting than one fold reads — the watermark stops at the last one measured")
	}
	var ticks []string
	for _, r := range runs.Runs {
		if r.Workflow == "scheduler" {
			ticks = append(ticks, r.StartedAt)
		}
	}
	items := ReadClosedItems(f.Reader, f.Repo, prior.QueueFoldedThrough, f.Now, ticks)
	if items.Error != "" {
		f.Log(items.Error + " — the outcome, park and latency rows are unchanged this run")
	}
	now := f.Now
	text := RenderTasksUsageFile(EncodeTasksUsage(FoldTasksUsage(TasksFoldIn{
		Prior: prior, Today: sliceUnits(now, 0, 10), Now: &now, Generated: now, MinuteRate: f.MinuteRate,
		Runs: runs.Runs, RunsFoldedThrough: runs.Watermark, Items: items.Records, QueueFoldedThrough: items.Watermark,
	})))
	summary := strconv.Itoa(len(runs.Runs)) + " run(s) and " + strconv.Itoa(len(items.Records)) + " closed item(s)"
	if f.unchanged(TasksUsagePath, text) {
		return Folded{Summary: summary + " — byte-identical"}, nil
	}
	return Folded{Files: []File{{TasksUsagePath, text}}, Moves: moves, Summary: summary}, nil
}

// Halves are the fold's two halves, sessions first.
func (f Fold) Halves() []Half {
	return []Half{{"sessions", f.FoldSessions}, {"machinery", f.FoldMachinery}}
}
