// Package signals collects what one task's preconditions read, at the
// moment a verdict is asked for: exactly the union the task's terms
// declare, so a frequent task never pays for a rarer one's reads. The run
// history comes first: it is what the cadence terms read and what sets
// the window every other collector reads over, since the task's newest
// run started (its cadence plus an hour of slack when it has none in the
// horizon). A read that fails is an error under the signal's name, never
// an empty signal; the term over it then fails loud.
package signals

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/jsregex"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

// HorizonDays is how far back the run history reads: the longest any
// cadence term looks, a month, plus slack.
const HorizonDays = 40

const day = 24 * time.Hour

// prFilePages caps the per-open-PR file read: past it a PR is a mass
// refactor no consumer rules on.
const prFilePages = 3

// LocalPackRoot is where a repository's own packs live.
const LocalPackRoot = ".claudinite/local/packs/"

func mustJS(body, flags string) *jsregex.Regex {
	r, err := jsregex.Compile(body, flags)
	if err != nil {
		panic(err)
	}
	return r
}

var (
	// housekeeping is the machinery's own commits and pull requests.
	housekeeping = mustJS(`\[skip ci\]|(^|\n)\s*baselin(e|ing)\b|claudinite[ -](baselin|maintenance|growth|task|work)|seed default-on`, "i")
	approvalRE   = mustJS(`^\s*\/claude\s+go\b`, "im")
	queueTitleRE = mustJS(`^\[claudinite-(task|work|schedule)\]`, "")
	trackerRE    = mustJS(`^(claudinite tracker:|\[claudinite\] ci performance$|auto-improvements tracker\b|repo tidy tracker$)`, "i")
	logStampRE   = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})T(\d{2})(\d{2})Z(?:-\d+)?--(?:pr|issue)-\d+--.+\.jsonl$`)
	sharedPackRE = regexp.MustCompile(`^\.claudinite/shared/packs/([^/]+)/`)
)

// IsSubstantiveCommit reports genuine project work: not a bot's, not a
// task's (its trailer), not corpus-only, not housekeeping.
func IsSubstantiveCommit(author, message string, files []string) bool {
	if strings.HasSuffix(author, "[bot]") {
		return false
	}
	if workitem.TaskFromMessage(message) != "" {
		return false
	}
	if len(files) > 0 {
		corpus := true
		for _, f := range files {
			if !strings.HasPrefix(f, ".claudinite/") {
				corpus = false
			}
		}
		if corpus {
			return false
		}
	}
	return !housekeeping.Test(message)
}

// Local are the checkout's facts a collector may not fetch for itself.
type Local struct {
	ManifestVersion      *string
	ShipsReleasePipeline bool
	RetentionDays        *float64
	// RetentionUnreadable is a retention_days some pack declares as
	// neither null nor a finite number, while none declares a number.
	RetentionUnreadable bool
}

var manifestPaths = []string{"manifest.json", "src/manifest.json", "public/manifest.json", "dist/manifest.json"}

var manifestPathRE = regexp.MustCompile(`(?m)^manifest_path=(.+)$`)

// manifestCandidates is the manifest the release config names, or the usual
// places when it names none.
func manifestCandidates(root string) []string {
	if raw, err := os.ReadFile(filepath.Join(root, ".github/release.config")); err == nil {
		if m := manifestPathRE.FindSubmatch(raw); m != nil {
			return []string{strings.TrimSpace(string(m[1]))}
		}
	}
	return manifestPaths
}

var shipsPipelineRE = regexp.MustCompile(`(?m)^(?:name:\s*['"]?(?:Release to Chrome Store|Release)['"]?\s*|manifest_path=.*)$`)

// ReadLocal reads the checkout's facts once per collector.
func ReadLocal(root string, packIDs []string, packConfig func(string) map[string]any) Local {
	var l Local
	for _, p := range manifestCandidates(root) {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if v, ok := m["version"]; ok && v != nil && v != "" && v != false && v != 0.0 {
			s := fmt.Sprint(v)
			l.ManifestVersion = &s
			break
		}
	}
	var candidates []string
	if es, err := os.ReadDir(filepath.Join(root, ".github/workflows")); err == nil {
		for _, e := range es {
			if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml")) {
				candidates = append(candidates, filepath.Join(".github/workflows", e.Name()))
			}
		}
	}
	candidates = append(candidates, ".github/release.config")
	for _, rel := range candidates {
		if raw, err := os.ReadFile(filepath.Join(root, rel)); err == nil && shipsPipelineRE.Match(raw) {
			l.ShipsReleasePipeline = true
			break
		}
	}
	unreadable := false
	for _, id := range packIDs {
		raw, declared := packConfig(id)["retention_days"]
		if v, ok := raw.(float64); ok && !math.IsInf(v, 0) && !math.IsNaN(v) {
			l.RetentionDays = &v
			break
		}
		unreadable = unreadable || (declared && raw != nil)
	}
	l.RetentionUnreadable = l.RetentionDays == nil && unreadable
	return l
}

// Collector is bound to one run's repository context.
type Collector struct {
	Issues        world.Issues
	Repo          world.Repo
	DefaultBranch string
	// Packs are the declared pack ids; PackConfig a pack's config block.
	Packs      []string
	PackConfig func(string) map[string]any
	// EngineVersion and PackVersions are the mount's stamp.
	EngineVersion string
	PackVersions  map[string]string
	// Items is the queue where the caller already holds it (the
	// scheduler run); nil reads it.
	Items []workitem.Issue
	Local Local
	// Fleet answers the fleet signal; nil where no fleet reader exists.
	Fleet func(sinceISO string) (any, error)

	files map[string][]string
}

// window is the read window: since the newest run that actually ran,
// else the task's default.
func window(task taskspec.Task, runs *precondition.Runs, now time.Time) precondition.Window {
	since := now.Add(-precondition.DefaultWindow(task.Decl))
	if runs != nil {
		for _, r := range runs.List {
			if r.NeverRan() {
				continue
			}
			if r.CreatedAt != nil {
				if t, ok := calendar.ParseInstant(*r.CreatedAt); ok {
					since = t
				}
			}
			break
		}
	}
	return precondition.Window{SinceISO: calendar.ISO(since), Days: float64(now.Sub(since)) / float64(day)}
}

// Collect gathers the task's declared signals for one verdict. only, when
// non-nil, narrows the collection to those names (the scheduler's cheap
// first pass asks for the run history alone).
func (c *Collector) Collect(task taskspec.Task, now time.Time, item *workitem.Issue, only []string) precondition.Signals {
	names := taskspec.Signals(task.Decl.Preconditions(), task.Terms)
	var rest []string
	for _, n := range names {
		if n != "runs" && (only == nil || has(only, n)) {
			rest = append(rest, n)
		}
	}
	var out precondition.Signals
	needsHistory := has(names, "runs")
	for _, n := range rest {
		if n != "request" {
			needsHistory = true
		}
	}
	var w precondition.Window
	if needsHistory {
		runs := c.runs(task, now, item)
		var ok *precondition.Runs
		if runs.Error == "" {
			ok = runs
		}
		w = window(task, ok, now)
		runs.Window = &w
		out.Runs = runs
	}
	if len(rest) == 0 {
		return out
	}
	c.collect(&out, rest, task, now, w, item)
	return out
}

func (c *Collector) collect(out *precondition.Signals, names []string, task taskspec.Task, now time.Time, w precondition.Window, item *workitem.Issue) {
	var commits []precondition.Commit
	var commitsErr error
	commitsRead := false
	windowCommits := func() ([]precondition.Commit, error) {
		if !commitsRead {
			commits, commitsErr = c.windowCommits(w.SinceISO)
			commitsRead = true
		}
		return commits, commitsErr
	}
	extra := func(name string, v any, err error) {
		if out.Extra == nil {
			out.Extra = map[string]any{}
		}
		if err != nil {
			out.Extra[name] = map[string]string{"error": err.Error()}
			return
		}
		out.Extra[name] = v
	}
	for _, name := range names {
		switch name {
		case "commits":
			list, err := windowCommits()
			if err != nil {
				out.Commits = &precondition.Commits{Error: err.Error()}
				continue
			}
			sub := false
			paths := []string{}
			seen := map[string]bool{}
			for _, cm := range list {
				sub = sub || cm.Substantive
				for _, f := range cm.Files {
					if !seen[f] {
						seen[f] = true
						paths = append(paths, f)
					}
				}
			}
			out.Commits = &precondition.Commits{List: list, Count: float64(len(list)), SubstantiveChange: sub, TouchedPaths: paths}
		case "prs":
			prs, err := c.prs(w.SinceISO)
			if err != nil {
				prs = &precondition.PRs{Error: err.Error()}
			}
			out.PRs = prs
		case "issues":
			issues, err := c.issues(w.SinceISO)
			if err != nil {
				issues = &precondition.Issues{Error: err.Error()}
			}
			out.Issues = issues
		case "conversationLogs":
			logs, err := c.logs(now)
			if err != nil {
				logs = &precondition.Logs{Error: err.Error()}
			}
			out.ConversationLogs = logs
		case "sharedMount":
			list, err := windowCommits()
			if err != nil {
				out.SharedMount = &precondition.Mount{Error: err.Error()}
				continue
			}
			changed := []string{}
			for _, cm := range list {
				for _, f := range cm.Files {
					if m := sharedPackRE.FindStringSubmatch(f); m != nil && has(c.Packs, m[1]) && !has(changed, m[1]) {
						changed = append(changed, m[1])
					}
				}
			}
			out.SharedMount = &precondition.Mount{ChangedPacks: changed}
		case "request":
			out.Request = c.request(item)
		case "localPacks":
			list, err := windowCommits()
			moved := false
			for _, cm := range list {
				for _, f := range cm.Files {
					moved = moved || strings.HasPrefix(f, LocalPackRoot)
				}
			}
			extra(name, map[string]bool{"changedInWindow": moved}, err)
		case "stamp":
			list, err := windowCommits()
			moved := false
			for _, cm := range list {
				for _, f := range cm.Files {
					moved = moved || strings.HasPrefix(f, ".claudinite/shared/")
				}
			}
			var engine any
			if c.EngineVersion != "" {
				engine = c.EngineVersion
			}
			versions := c.PackVersions
			if versions == nil {
				versions = map[string]string{}
			}
			extra(name, map[string]any{"present": c.EngineVersion != "" || len(versions) > 0, "engineVersion": engine,
				"packVersions": versions, "convergedInWindow": moved, "canonHead": nil}, err)
		case "release":
			tag, err := c.Repo.LatestRelease()
			var latest any
			switch {
			case err == nil:
				latest = tag
			case errors.Is(err, world.ErrGone):
				err = nil
			}
			var mv any
			if c.Local.ManifestVersion != nil {
				mv = *c.Local.ManifestVersion
			}
			extra(name, map[string]any{"latestTag": latest, "manifestVersion": mv, "shipsPipeline": c.Local.ShipsReleasePipeline}, err)
		case "branches":
			v, err := c.branches(w.SinceISO)
			extra(name, v, err)
		case "queue":
			open, err := c.openQueue()
			extra(name, map[string]any{"open": open}, err)
		case "fleet":
			if c.Fleet == nil {
				extra(name, nil, nil)
				continue
			}
			v, err := c.Fleet(w.SinceISO)
			extra(name, v, err)
		}
	}
}

func (c *Collector) runs(task taskspec.Task, now time.Time, item *workitem.Issue) *precondition.Runs {
	title := workitem.Title{Pack: task.Pack, Task: task.ID}.String()
	items := c.Items
	if items == nil {
		horizon := calendar.ISO(now.Add(-HorizonDays * day))
		var err error
		items, err = listAll(c.Issues, world.Query{State: "all", Since: horizon, Sort: "created", Direction: "desc"})
		if err != nil {
			return &precondition.Runs{Error: err.Error(), List: []precondition.Run{}}
		}
	}
	list := []precondition.Run{}
	for _, i := range items {
		if strings.TrimSpace(i.Title) != title || (item != nil && i.Number == item.Number) {
			continue
		}
		unpicked := false
		for _, s := range workitem.StatusesOn(i.Labels) {
			if s == workitem.StatusBlocked || s == workitem.StatusReady {
				unpicked = true
			}
		}
		if unpicked {
			continue
		}
		r := precondition.Run{Number: i.Number, State: i.State, Woken: workitem.ParseFields(i.Body).Woken != nil}
		r.CreatedAt = strOrNil(i.CreatedAt)
		r.ClosedAt = strOrNil(i.ClosedAt)
		r.Status = strOrNil(i.Status())
		r.Park = strOrNil(i.ParkKind())
		r.Outcome = strOrNil(i.Outcome())
		list = append(list, r)
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Number > list[b].Number })
	return &precondition.Runs{List: list, HorizonDays: HorizonDays}
}

func listAll(gh world.Issues, q world.Query) ([]workitem.Issue, error) {
	var out []workitem.Issue
	for page := 1; ; page++ {
		got, err := gh.IssuesPage(q, page)
		if err != nil {
			return nil, fmt.Errorf("the work-item list could not be read at page %d (%v)", page, err)
		}
		for _, i := range got {
			if !i.PullRequest && i.IsQueueItem() {
				out = append(out, i.Issue)
			}
		}
		if len(got) < world.PageSize {
			return out, nil
		}
	}
}

func (c *Collector) openQueue() ([]workitem.Issue, error) {
	if c.Items != nil {
		out := []workitem.Issue{}
		for _, i := range c.Items {
			if i.State == "open" {
				out = append(out, i)
			}
		}
		return out, nil
	}
	return listAll(c.Issues, world.Query{State: "open", Sort: "created", Direction: "desc"})
}

func (c *Collector) commitFiles(sha string) ([]string, error) {
	if c.files == nil {
		c.files = map[string][]string{}
	}
	if f, ok := c.files[sha]; ok {
		return f, nil
	}
	d, err := c.Repo.Commit(sha)
	if err != nil {
		return nil, err
	}
	c.files[sha] = d.Files
	return d.Files, nil
}

func (c *Collector) windowCommits(since string) ([]precondition.Commit, error) {
	out := []precondition.Commit{}
	for page := 1; ; page++ {
		refs, err := c.Repo.CommitsPage(c.DefaultBranch, since, page)
		if err != nil {
			return nil, fmt.Errorf("the commit history could not be read at page %d (%v)", page, err)
		}
		for _, r := range refs {
			files, err := c.commitFiles(r.SHA)
			if err != nil {
				return nil, fmt.Errorf("commit %s could not be read (%v)", r.SHA, err)
			}
			if files == nil {
				files = []string{}
			}
			cm := precondition.Commit{SHA: r.SHA, Message: r.Message, Author: strOrNil(r.Author),
				Task: strOrNil(workitem.TaskFromMessage(r.Message)), Substantive: IsSubstantiveCommit(r.Author, r.Message, files), Files: files}
			out = append(out, cm)
		}
		if len(refs) < world.PageSize {
			return out, nil
		}
	}
}

func after(at, since string) bool {
	a, ok1 := calendar.ParseInstant(at)
	s, ok2 := calendar.ParseInstant(since)
	return ok1 && ok2 && !a.Before(s)
}

func (c *Collector) pulls(state, sortBy string, keep func(world.Pull) bool) ([]world.Pull, error) {
	var out []world.Pull
	for page := 1; ; page++ {
		got, err := c.Repo.PullsPage(state, sortBy, "desc", page)
		if err != nil {
			return nil, fmt.Errorf("the %s pull requests could not be read at page %d (%v)", state, page, err)
		}
		stop := false
		for _, p := range got {
			if keep != nil && !keep(p) {
				stop = true
				break
			}
			out = append(out, p)
		}
		if stop || len(got) < world.PageSize {
			return out, nil
		}
	}
}

func mineable(p world.Pull) bool {
	return !strings.HasSuffix(p.Author, "[bot]") && !housekeeping.Test(strings.TrimSpace(p.Title))
}

func (c *Collector) prs(since string) (*precondition.PRs, error) {
	open, err := c.pulls("open", "updated", nil)
	if err != nil {
		return nil, err
	}
	closed, err := c.pulls("closed", "updated", func(p world.Pull) bool { return after(p.UpdatedAt, since) })
	if err != nil {
		return nil, err
	}
	var merged, inWindow []world.Pull
	for _, p := range closed {
		if p.MergedAt != "" && after(p.MergedAt, since) && mineable(p) {
			merged = append(merged, p)
		}
	}
	for _, p := range open {
		if after(p.UpdatedAt, since) {
			inWindow = append(inWindow, p)
		}
	}
	taskAuthored := map[int]bool{}
	for _, p := range append(append([]world.Pull{}, inWindow...), merged...) {
		if _, ok := taskAuthored[p.Number]; ok {
			continue
		}
		if p.HeadSHA == "" {
			taskAuthored[p.Number] = false
			continue
		}
		head, err := c.Repo.Commit(p.HeadSHA)
		taskAuthored[p.Number] = err == nil && workitem.TaskFromMessage(head.Message) != ""
	}
	out := &precondition.PRs{Open: []precondition.OpenPR{}, Touched: []int{}, Merged: []precondition.MergedPR{}}
	for _, p := range merged {
		if !taskAuthored[p.Number] {
			out.Merged = append(out.Merged, precondition.MergedPR{Number: p.Number, Title: p.Title, MergedAt: p.MergedAt})
		}
	}
	for _, p := range open {
		var paths []string
		for page := 1; page <= prFilePages; page++ {
			got, err := c.Repo.PullFilesPage(p.Number, page)
			if err != nil {
				break
			}
			paths = append(paths, got...)
			if len(got) < world.PageSize {
				break
			}
		}
		if len(paths) == 0 {
			paths = nil
		}
		out.Open = append(out.Open, precondition.OpenPR{Number: p.Number, Title: p.Title, UpdatedAt: p.UpdatedAt, ChangedPaths: paths})
	}
	for _, p := range inWindow {
		if !taskAuthored[p.Number] {
			out.Touched = append(out.Touched, p.Number)
		}
	}
	return out, nil
}

func (c *Collector) issues(since string) (*precondition.Issues, error) {
	var real []world.Issue
	for page := 1; ; page++ {
		got, err := c.Issues.IssuesPage(world.Query{State: "open", Sort: "updated", Direction: "desc"}, page)
		if err != nil {
			return nil, fmt.Errorf("the open issues could not be read at page %d (%v)", page, err)
		}
		for _, i := range got {
			if i.PullRequest || queueTitleRE.Test(i.Title) || trackerRE.Test(strings.TrimSpace(i.Title)) {
				continue
			}
			real = append(real, i)
		}
		if len(got) < world.PageSize {
			break
		}
	}
	out := &precondition.Issues{Open: []precondition.OpenIssue{}, Touched: []int{}}
	for _, i := range real {
		labels := []string(i.Labels)
		if labels == nil {
			labels = []string{}
		}
		out.Open = append(out.Open, precondition.OpenIssue{Number: i.Number, Title: i.Title, UpdatedAt: i.UpdatedAt, Labels: labels})
		if after(i.UpdatedAt, since) {
			out.Touched = append(out.Touched, i.Number)
		}
	}
	return out, nil
}

func (c *Collector) logs(now time.Time) (*precondition.Logs, error) {
	out := &precondition.Logs{RetentionDays: c.Local.RetentionDays, RetentionUnreadable: c.Local.RetentionUnreadable}
	if _, err := c.Repo.Branch("conversation-logs"); err != nil {
		if errors.Is(err, world.ErrGone) {
			return out, nil
		}
		return nil, err
	}
	out.Present = true
	paths, err := c.Repo.TreePaths("conversation-logs")
	if err != nil {
		paths = nil
	}
	var stamps []time.Time
	for _, p := range paths {
		m := logStampRE.FindStringSubmatch(p)
		if m == nil {
			continue
		}
		if t, err := time.Parse("2006-01-02T15:04Z", m[1]+"T"+m[2]+":"+m[3]+"Z"); err == nil {
			stamps = append(stamps, t)
		}
	}
	if len(stamps) == 0 {
		return out, nil
	}
	oldest, newest := stamps[0], stamps[0]
	for _, s := range stamps {
		if s.Before(oldest) {
			oldest = s
		}
		if s.After(newest) {
			newest = s
		}
	}
	age := func(t time.Time) *float64 { v := float64(now.Sub(t)) / float64(day); return &v }
	out.OldestLogAgeDays, out.NewestLogAgeDays, out.LogCount = age(oldest), age(newest), len(stamps)
	return out, nil
}

func (c *Collector) branches(since string) (any, error) {
	var list []world.Branch
	for page := 1; ; page++ {
		got, err := c.Repo.BranchesPage(page)
		if err != nil {
			return nil, fmt.Errorf("the branches could not be read at page %d (%v)", page, err)
		}
		list = append(list, got...)
		if len(got) < world.PageSize {
			break
		}
	}
	dates := map[string]string{}
	for _, b := range list {
		if _, ok := dates[b.SHA]; ok || b.SHA == "" {
			continue
		}
		d, err := c.Repo.Commit(b.SHA)
		if err == nil {
			dates[b.SHA] = d.Date
		} else {
			dates[b.SHA] = ""
		}
	}
	names := []string{}
	entries := []map[string]any{}
	touched := []string{}
	for _, b := range list {
		names = append(names, b.Name)
		entries = append(entries, map[string]any{"name": b.Name, "updatedAt": strOrNil(dates[b.SHA])})
		if after(dates[b.SHA], since) {
			touched = append(touched, b.Name)
		}
	}
	return map[string]any{"names": names, "list": entries, "touched": touched}, nil
}

func (c *Collector) request(item *workitem.Issue) *precondition.Request {
	if item == nil {
		return nil
	}
	n := workitem.ParseBody(item.Body).Request
	if n == 0 {
		return nil
	}
	i, err := c.Issues.Issue(n)
	if errors.Is(err, world.ErrGone) {
		return &precondition.Request{Number: n, Gone: true}
	}
	if err != nil {
		return &precondition.Request{Number: n, Unreadable: true, Error: "the issues API answered " + err.Error()}
	}
	perms := map[string]string{}
	failed := ""
	permissionOf := func(login string) string {
		if login == "" {
			return "none"
		}
		if p, ok := perms[login]; ok {
			return p
		}
		role, err := c.Issues.Permission(login)
		switch {
		case err == nil:
		case errors.Is(err, world.ErrGone):
			role = "none"
		default:
			failed = fmt.Sprintf("the permission API answered %v for @%s", err, login)
			role = "none"
		}
		perms[login] = role
		return role
	}
	out := &precondition.Request{Number: n, State: i.State, Author: i.Author, AuthorPermission: permissionOf(i.Author),
		Labels: append([]string{}, i.Labels...), Approvals: []precondition.Approval{}}
	for _, l := range i.Labels {
		if l == workitem.OriginAdHoc || l == workitem.QueuedLabel || l == workitem.RequestLabel {
			out.Queued = true
		}
	}
	comments, err := c.Issues.Comments(n)
	if err != nil {
		return &precondition.Request{Number: n, Unreadable: true, Error: "the comments API answered " + err.Error()}
	}
	for _, cm := range comments {
		if cm.Author == "" || !approvalRE.Test(cm.Body) {
			continue
		}
		out.Approvals = append(out.Approvals, precondition.Approval{Login: cm.Author, Permission: permissionOf(cm.Author)})
	}
	if failed != "" {
		return &precondition.Request{Number: n, Unreadable: true, Error: failed}
	}
	return out
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
