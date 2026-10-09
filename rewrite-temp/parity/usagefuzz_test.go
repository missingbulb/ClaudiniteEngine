package parity

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The usage face's fuzz: worlds drawn from a seeded generator, each run by
// both engines and compared as a fixture is, with no recorded answer. The
// generator reaches every source a fold reads, every shape of entry its
// counters select on and every way a source fails; CLAUDINITE_PARITY_FUZZ
// sets how many worlds (default 40), CLAUDINITE_PARITY_FUZZ_SEED the
// first seed (default 1) and CLAUDINITE_PARITY_FUZZ_DUMP a directory each
// world and the Node answer are written to. A failing world is written to the temporary
// directory, ready to become a fixture.

const (
	usageFuzzEnv     = "CLAUDINITE_PARITY_FUZZ"
	usageFuzzSeedEnv = "CLAUDINITE_PARITY_FUZZ_SEED"
	usageFuzzDumpEnv = "CLAUDINITE_PARITY_FUZZ_DUMP"
)

type usageGen struct {
	r   *rand.Rand
	now time.Time
}

func (g usageGen) chance(p float64) bool { return g.r.Float64() < p }

func (g usageGen) pick(options ...string) string { return options[g.r.IntN(len(options))] }

func (g usageGen) pickAny(options ...any) any { return options[g.r.IntN(len(options))] }

func (g usageGen) between(from, to time.Time) time.Time {
	span := to.Sub(from)
	if span <= 0 {
		return from
	}
	return from.Add(time.Duration(g.r.Int64N(int64(span))))
}

func iso(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

func isoMs(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func text(s string) *string { return &s }

func entryJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// The transcript shapes the counters select on, as captured transcripts
// carry them.
func (g usageGen) entry(at time.Time) map[string]any {
	user := func(content any) map[string]any {
		return map[string]any{"type": "user", "message": map[string]any{"content": content}}
	}
	asst := func(blocks ...any) map[string]any {
		return map[string]any{"type": "assistant", "message": map[string]any{"content": blocks}}
	}
	use := func(id, name string, input map[string]any) map[string]any {
		return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
	}
	result := func(id string, content any) map[string]any {
		return user([]any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content}})
	}
	hook := func(lines ...string) map[string]any {
		return map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"content": strings.Join(lines, "\n")}}
	}
	stamp := iso(at)
	run := strconv.Itoa(g.r.IntN(9000) + 1)
	line := func(h, msg string) string { return stamp + " run=" + run + " " + h + ": " + msg }
	skill := g.pick("acme-skill-h", "acme-skill-i", "acme-skill-f", "never-mounted")
	exec := "claudinite-task-exec v1 " + g.pick("acme-pack/acme-task", "acme-pack/usage-fold", "acme-local/acme-task-b") +
		" [" + g.pick("d2026-08-06", "d2026-08-07", "#42") + "] " + g.pick("success", "failed", "task-gone", "invalid")
	id := "t" + strconv.Itoa(g.r.IntN(50))
	var e map[string]any
	switch g.r.IntN(31) {
	case 27:
		e = asst(map[string]any{"type": "tool_use", "id": map[string]any{"odd": 1}, "name": g.pick("Read", "Bash", "Skill")})
	case 28:
		e = user([]any{map[string]any{"type": "text", "text": exec}, map[string]any{"type": "tool_result", "tool_use_id": map[string]any{}, "content": nil}})
	case 29:
		e = map[string]any{"type": "assistant", "message": map[string]any{"content": []any{},
			"usage": g.pickAny([]any{"x", 1}, map[string]any{"input_tokens": 0, "output_tokens": 0}, map[string]any{"input_tokens": "7"})}}
	case 30:
		e = asst(map[string]any{"type": "text", "text": "x"})
		e["timestamp"] = g.pick("not a time", "2026-13-45T99:00:00Z", "")
		return e
	case 0, 1, 2:
		e = map[string]any{"type": "user", "origin": map[string]any{"kind": "human"}, "promptSource": "sdk", "userType": "external",
			"message": map[string]any{"content": g.pick("do the thing", "run /acme-skill-h when you are done", "lgtm", exec)}}
	case 3:
		e = result(id, "ok")
	case 4:
		e = map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"content": "<system-reminder>…</system-reminder>"}}
	case 5:
		e = map[string]any{"type": "user", "isSidechain": true, "message": map[string]any{"content": "go"}}
	case 6:
		e = map[string]any{"type": "user", "origin": map[string]any{"kind": "task-notification", "subkind": g.pick("scheduled-trigger", "")},
			"message": map[string]any{"content": "Execute the Claudinite executor"}}
	case 7:
		e = map[string]any{"type": "user", "isCompactSummary": true, "isVisibleInTranscriptOnly": true,
			"message": map[string]any{"content": "This session is being continued from a previous conversation…"}}
	case 8:
		name := g.pick("acme-skill-h", "acme-skill-i", "model", "clear", "review")
		e = user("<command-name>/" + name + "</command-name>\n<command-message>" + name + "</command-message>\n<command-args>" + g.pick("", "acme-model-large") + "</command-args>")
	case 9:
		e = user("<local-command-stdout>Set model to acme-model-large</local-command-stdout>")
	case 10:
		e = asst(use(id, "Skill", map[string]any{"skill": skill}))
		if g.chance(0.3) {
			e["isSidechain"] = true
		}
	case 11:
		e = asst(map[string]any{"type": "text", "text": g.pick("sure", "the run failed: "+exec, "use /review")})
	case 12:
		e = asst(use(id, g.pick("Read", "Edit", "Write", "Grep"), map[string]any{"file_path": g.pick(
			"/x", ".claudinite/shared/packs/acme-pack/skills/"+skill+"/SKILL.md", ".claudinite/shared/packs/acme-pack/RULES.md",
			"packs/acme-pack/skills/acme-skill-h/SKILL.md", "src/a.txt")}))
	case 13:
		e = hook("Stop hook feedback:\n[node $CLAUDE_PROJECT_DIR/engine/hooks/stop-command.mjs]: "+line("Stop", "start checks"),
			"Claudinite conformance checks failed — fix these findings now, in this session:", "",
			"[BLOCKING] "+g.pick("comment-classification", "task-lifecycle", "acme-check")+"  (conversation)",
			"  the reply declares no line", "", "[ADVISORY] acme-skill-g  packs/x/y.mjs:3", "",
			"1 blocking, 1 advisory (work scope: all vs origin/main).", line("Stop", "done exit=2 blocking-findings"))
	case 14:
		e = map[string]any{"type": "system", "subtype": "stop_hook_summary", "hookErrors": []any{
			line("Stop", "start checks") + "\n[BLOCKING] task-lifecycle  (branch)\n1 blocking, 0 advisory (work scope: all vs origin/main).\n" +
				line("Stop", "done exit=2 blocking-findings")}}
	case 15:
		e = map[string]any{"type": "attachment", "attachment": map[string]any{"type": "hook_success", "hookName": "Stop", "hookEvent": "Stop",
			"stderr": line("Stop", "start checks") + "\n" + line("Stop", g.pick("done exit=0 checks-passed", "done exit=0 loop-guard-relent", "done exit=2 runner-error")) + "\n",
			"stdout": "", "exitCode": 0}}
	case 16:
		cmd := g.pick("node engine/checks/check_the_world.mjs", "node .claudinite/shared/engine/checks/check_the_work.mjs >/tmp/out",
			"npm test", "node engine/checks/check_the_world.mjs | tail -3; node engine/checks/check_the_work.mjs", "git ls-files | grep check_the_world")
		e = asst(use(id, "Bash", map[string]any{"command": cmd}))
	case 17:
		out := g.pick("", "0 blocking, 7 advisory (world scope: all vs origin/main).",
			"[BLOCKING] task-lifecycle  (branch)\n  …\n1 blocking, 4 advisory (world scope: all vs origin/main).", "brief...\n"+exec+"\n")
		e = result(id, out)
		e["toolUseResult"] = map[string]any{"stdout": out, "stderr": "", "interrupted": false, "isImage": false}
	case 18:
		e = asst(use(id, "mcp__github__get_job_logs", map[string]any{"job_id": 1}))
	case 19:
		at := g.pick("08:12:05", "09:30:11")
		logs := "2026-07-29T08:12:04.1234567Z ##[group]Run node engine/checks/check_the_world.mjs\n" +
			"2026-07-29T" + at + ".7654321Z [BLOCKING] task-lifecycle  (branch)\n" +
			"2026-07-29T" + at + ".7654323Z 1 blocking, 4 advisory (world scope: all vs origin/main).\n"
		body := entryJSON(map[string]any{"job_id": 90518898761, "logs_content": logs})
		e = result(id, []any{map[string]any{"type": "text", "text": body}})
		e["toolUseResult"] = []any{map[string]any{"type": "text", "text": body}}
	case 20:
		usage := map[string]any{"input_tokens": g.r.IntN(500), "output_tokens": g.r.IntN(80)}
		if g.chance(0.5) {
			usage["cache_read_input_tokens"] = g.r.IntN(5000)
		}
		if g.chance(0.3) {
			usage["cache_creation_input_tokens"] = g.r.IntN(900)
		}
		msg := map[string]any{"content": []any{}, "usage": usage}
		if g.chance(0.7) {
			msg["model"] = g.pick("acme-model-large", "acme-model-small")
		}
		e = map[string]any{"type": "assistant", "message": msg}
	case 21:
		e = hook(g.pick("[cn] build started ok 3ms", "[cn] build cached ok 2ms", "# Claudinite engine 1.61005.1"),
			g.pick("[cn] buildwait stop ok "+strconv.Itoa(g.r.IntN(9000))+"ms", "[cn] buildwait check timeout 600000ms", ""),
			g.pick("[cn] build compiled ok "+strconv.Itoa(g.r.IntN(9000))+"ms", "[cn] build compiled error 900ms", "[cn] checks stop ok 4ms"))
	case 22:
		e = hook(line("Stop", "claudinite-check-timing v1 "+g.pick("work", "world")+" total="+strconv.Itoa(g.r.IntN(2000))+
			" acme-check-b="+strconv.Itoa(g.r.IntN(900))+" acme-check-c="+strconv.Itoa(g.r.IntN(200))))
	case 23:
		e = hook(line("PreToolUse", g.pick("done exit=2 skill-not-loaded-for-call Bash needs "+skill, "advisory action-guard acme-check",
			"done exit=2 action-guard no-pr-polling")))
	case 24:
		e = hook(line(g.pick("PostToolUse", "UserPromptSubmit"), "skill-trigger "+g.pick("WebFetch", "prompt")+" "+skill))
	case 25:
		e = user(exec)
	default:
		e = asst(use(id, "Bash", map[string]any{"command": "ls"}), use(id+"b", "Skill", map[string]any{"skill": skill}))
	}
	if g.chance(0.7) {
		e["timestamp"] = isoMs(at)
	}
	return e
}

func (g usageGen) captureText(start time.Time) string {
	var lines []string
	at := start
	for range g.r.IntN(14) {
		at = at.Add(time.Duration(g.r.IntN(900)) * time.Second)
		if g.chance(0.04) {
			at = at.Add(11 * time.Hour)
		}
		if g.chance(0.05) {
			lines = append(lines, `{"type": "user", "message": {"content": "a partial wri`)
			continue
		}
		lines = append(lines, entryJSON(g.entry(at)))
	}
	return strings.Join(lines, "\n") + "\n"
}

func (g usageGen) logs() map[string]string {
	if g.chance(0.15) {
		return nil
	}
	out := map[string]string{"README.md": "# logs\n"}
	if g.chance(0.2) {
		out["notes.jsonl"] = "{}\n"
	}
	for range g.r.IntN(9) {
		day := g.now.AddDate(0, 0, -g.r.IntN(36)).Truncate(24 * time.Hour)
		at := day.Add(time.Duration(g.r.IntN(24*60)) * time.Minute)
		if at.After(g.now) {
			at = g.now.Add(-time.Minute)
		}
		key := g.pick("issue-0", "issue-"+strconv.Itoa(g.r.IntN(30)+1), "pr-"+strconv.Itoa(g.r.IntN(30)+1500))
		suffix := ""
		if g.chance(0.15) {
			suffix = "-2"
		}
		name := at.UTC().Format("2006-01-02T1504Z") + suffix + "--" + key + "--s" + strconv.Itoa(g.r.IntN(5)+1) + ".jsonl"
		out[name] = g.captureText(at)
	}
	return out
}

// The usage file a member carried before the fold moved home, in the
// first format's shape.
const usageV1 = `{"version": 1, "foldedThrough": "%s", "runsFoldedThrough": "%sT03:00:00Z", "days": {},
 "weeks": {"%s": {"days": 3, "captures": 4, "merges": 4, "sessionDays": 3, "userMessages": 40, "userCommands": 1,
  "skillLoads": {"acme-skill-h": 3}, "checks": {"work": {"runs": 20, "failures": 5, "errors": 0, "blocking": 8, "advisory": 0, "ciRuns": 0, "ciFailures": 0}},
  "checkFindings": {"task-lifecycle": {"blocking": 8, "advisory": 0}},
  "tasks": {"acme-pack/acme-task": {"agent": 3, "code-work": 0, "skipped": 15, "failed": 0, "deferred": 0}}}}}
`

func (g usageGen) main() []usageCommit {
	skills := map[string]*string{
		".claudinite/shared/packs/acme-pack/skills/acme-skill-h/SKILL.md": text("# h\n"),
		".claudinite/local/packs/acme-local/skills/acme-skill-i/SKILL.md": text("# i\n"),
		".claudinite/shared/packs/acme-pack/skills/no-skill-file/README":  text("x\n"),
		"README.md": text("hi\n"),
	}
	switch g.r.IntN(5) {
	case 0:
		folded := iso(g.now.AddDate(0, 0, -3))[:10]
		y, w := g.now.AddDate(0, 0, -3).ISOWeek()
		skills[".claudinite/local/usage.GENERATED.json"] = text(fmt.Sprintf(usageV1, folded, folded, fmt.Sprintf("%d-W%02d", y, w)))
	case 1:
		skills[".claudinite/usage/sessions-and-elements.json"] = text("{not json\n")
	case 2:
		skills[".claudinite/local/tasks-usage.GENERATED.json"] = text(`{"version": 1, "generated": "x", "runsFoldedThrough": "` + iso(g.now.AddDate(0, 0, -1)) + `"}` + "\n")
	}
	commits := []usageCommit{{Date: iso(g.now.AddDate(0, 0, -45)), Files: skills}}
	for i := range g.r.IntN(6) {
		at := g.between(g.now.AddDate(0, 0, -40), g.now.Add(-time.Hour))
		files := map[string]*string{}
		for j := range g.r.IntN(3) + 1 {
			p := "src/f" + strconv.Itoa(g.r.IntN(4)) + ".txt"
			if j == 0 && g.chance(0.2) {
				p = "logo.png"
				files[p] = text("\x89PNG\x00\x01" + strconv.Itoa(i))
				continue
			}
			files[p] = text(strings.Repeat("line "+strconv.Itoa(i)+"\n", g.r.IntN(20)+1))
			if g.chance(0.1) {
				files["name\twith\ttabs.txt"] = text("odd\n")
			}
		}
		commits = append(commits, usageCommit{Date: iso(at), Files: files})
	}
	sortCommits(commits)
	return commits
}

func sortCommits(cs []usageCommit) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].Date < cs[j-1].Date; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

func (g usageGen) costLine(workflow string, run int) string {
	phases := map[string][]string{"scheduler": {"list", "ask", "repair", "drain"}, "executor": {"pick", "claim", "code-work", "hand-off", "converge"}}
	parts := []string{"claudinite-run-cost v1 " + workflow + " [" + strconv.Itoa(run) + "]"}
	if g.chance(0.8) {
		parts = append(parts, "calls="+strconv.Itoa(g.r.IntN(60)))
	}
	for _, p := range phases[workflow] {
		if g.chance(0.7) {
			parts = append(parts, p+"="+strconv.Itoa(g.r.IntN(90000)))
		}
	}
	return strings.Join(parts, " ")
}

func (g usageGen) github() usageGitHub {
	gh := usageGitHub{Runs: map[string][]map[string]any{}, Jobs: map[string][]map[string]any{}, Logs: map[string]string{},
		Timelines: map[string][]any{}, Events: map[string][]any{}, Fail: map[string]string{}}
	runID, jobID := 9000, 70000
	for _, workflow := range []string{"scheduler", "executor"} {
		var runs []map[string]any
		count := g.r.IntN(7)
		if g.chance(0.08) {
			count = 30 + g.r.IntN(15)
		}
		for range count {
			runID++
			at := g.between(g.now.AddDate(0, 0, -4), g.now.Add(-time.Minute))
			run := map[string]any{"id": runID, "created_at": iso(at), "conclusion": g.pick("success", "success", "failure", "timed_out", "cancelled", "startup_failure")}
			if g.chance(0.85) {
				run["run_started_at"] = iso(at.Add(time.Duration(g.r.IntN(90)) * time.Second))
			}
			if g.chance(0.05) {
				run["conclusion"] = nil
			}
			if g.chance(0.04) {
				delete(run, "id")
			}
			if g.chance(0.05) && len(runs) > 0 {
				run["created_at"], run["run_started_at"] = runs[0]["created_at"], runs[0]["run_started_at"]
			}
			runs = append([]map[string]any{run}, runs...)
			if !g.chance(0.85) {
				continue
			}
			var jobs []map[string]any
			for range g.r.IntN(3) + 1 {
				jobID++
				from := at.Add(time.Duration(g.r.IntN(60)) * time.Second)
				job := map[string]any{"id": jobID, "name": "job", "started_at": iso(from), "conclusion": g.pick("success", "success", "skipped", "failure")}
				if g.chance(0.9) {
					job["completed_at"] = iso(from.Add(time.Duration(g.r.IntN(400)) * time.Second))
				} else {
					job["completed_at"] = nil
				}
				jobs = append(jobs, job)
				if workflow == "scheduler" && g.chance(0.7) {
					stamp := from.Format("2006-01-02T15:04:05.0000000Z")
					var lines []string
					for range g.r.IntN(3) {
						lines = append(lines, stamp+" "+g.costLine(workflow, runID))
					}
					gh.Logs[strconv.Itoa(jobID)] = (stamp + " ##[group]Run node cn tasks schedule\n" + strings.Join(lines, "\n") + "\n")
				}
			}
			gh.Jobs[strconv.Itoa(runID)] = jobs
		}
		if g.chance(0.03) {
			runs = append(runs, nil)
		}
		gh.Runs[workflow] = runs
	}

	tasks := []string{"acme-pack/acme-task", "acme-pack/usage-fold", "acme-local/acme-task-b"}
	labels := []string{"task:status:done", "task:status:rejected", "outcome:delivered", "outcome:done", "outcome:obsolete", "task:done",
		"task:status:needs-human-approval", "task:status:waiting-for-executor", "bug"}
	statuses := []string{"task:status:waiting-for-executor", "task:status:running-executor", "task:status:running-agent",
		"task:status:needs-human-failure", "task:status:needs-human-approval", "task:status:needs-human-decision",
		"task:needs-human-action", "needs-human", "task:status:needs-human-unknown", "task:status:done"}
	items := g.r.IntN(9)
	for n := 1; n <= items; n++ {
		created := g.between(g.now.AddDate(0, 0, -5), g.now.Add(-2*time.Hour))
		closed := g.between(created, g.now.Add(-time.Minute))
		title := "[claudinite-work] " + g.pick(tasks...)
		body := ""
		switch g.r.IntN(5) {
		case 0:
			title = "a person's own words"
			body = "someone's own words\n\n<!-- claudinite-item -->\npacks/acme-pack/tasks/acme-task/task.md\n<!-- /claudinite-item -->\n"
		case 1:
			title = "an ordinary bug"
		}
		var worn []any
		for range g.r.IntN(3) {
			worn = append(worn, map[string]any{"name": g.pick(labels...)})
		}
		issue := map[string]any{"number": n, "title": title, "body": body, "labels": orEmpty(worn), "state": g.pick("closed", "closed", "closed", "open"),
			"created_at": iso(created), "closed_at": iso(closed), "updated_at": iso(g.between(closed, g.now))}
		if g.chance(0.1) {
			issue["pull_request"] = map[string]any{}
		}
		if g.chance(0.1) {
			issue["closed_at"] = nil
		}
		gh.Issues = append(gh.Issues, issue)
		key := strconv.Itoa(n)
		if g.chance(0.8) {
			var events []any
			at := created
			for range g.r.IntN(6) {
				at = g.between(at, closed)
				events = append(events, map[string]any{"event": "labeled", "label": map[string]any{"name": g.pick(statuses...)}, "created_at": iso(at)})
			}
			if g.chance(0.6) {
				events = append(events, map[string]any{"event": "commented", "created_at": iso(closed),
					"body": "ran\n\n```\nclaudinite-task-exec v1 acme-pack/acme-task [#" + key + "] success\n" + g.costLine("executor", runID-g.r.IntN(5)) + "\n```"})
			}
			gh.Timelines[key] = events
		}
		if g.chance(0.8) {
			var events []any
			for range g.r.IntN(4) {
				events = append(events, map[string]any{"event": g.pick("labeled", "unlabeled"), "label": map[string]any{"name": g.pick(statuses...)},
					"created_at": iso(g.between(created, closed))})
			}
			gh.Events[key] = orEmpty(events)
		}
	}
	pulls := g.r.IntN(7)
	for n := 1500; n < 1500+pulls; n++ {
		created := g.between(g.now.AddDate(0, 0, -6), g.now.Add(-time.Hour))
		pr := map[string]any{"number": n, "created_at": iso(created), "updated_at": iso(g.between(created, g.now)),
			"body": g.pick("does the thing", "does the thing\n\nCloses #"+strconv.Itoa(g.r.IntN(9)+1)+"\n", "Fixes #3", "Refs #2", "")}
		if g.chance(0.8) {
			pr["merged_at"] = iso(g.between(created, g.now))
			if g.chance(0.05) {
				pr["merged_at"] = iso(created.Add(-time.Hour))
			}
		} else {
			pr["merged_at"] = nil
		}
		gh.Pulls = append(gh.Pulls, pr)
	}
	if g.chance(0.75) {
		gh.Releases = []map[string]any{}
		count := g.r.IntN(5)
		if g.chance(0.1) {
			count = 100 + g.r.IntN(5)
		}
		for range count {
			r := map[string]any{"tag_name": "v1", "created_at": iso(g.between(g.now.AddDate(0, 0, -40), g.now))}
			if g.chance(0.8) {
				r["published_at"] = iso(g.between(g.now.AddDate(0, 0, -40), g.now))
			}
			if g.chance(0.05) {
				delete(r, "created_at")
				delete(r, "published_at")
			}
			gh.Releases = append(gh.Releases, r)
		}
	}
	if g.chance(0.3) {
		gh.Fail[g.pick("/repos/acme/member/actions/workflows/claudinite-scheduler.yml/runs", "/repos/acme/member/issues?",
			"/repos/acme/member/pulls", "/repos/acme/member/releases", "/repos/acme/member/actions/runs/", "/repos/acme/member/issues/1/",
			"/repos/acme/member/actions/jobs/")] = g.pick("unreachable", "500", "403")
	}
	return gh
}

func (g usageGen) world() usageWorld {
	w := usageWorld{Now: isoMs(g.now), Repo: "acme/member", Pack: "claudinite-tasks", Task: "usage-fold",
		Packs:  []map[string]string{{"id": "acme-pack", "kind": "canon"}, {"id": "acme-local", "kind": "local"}, {"id": "acme-absent", "kind": "temp"}},
		Target: usageTarget{Branch: "claudinite/usage-fold"},
		Main:   g.main(), Logs: g.logs(), GitHub: g.github()}
	if g.chance(0.7) {
		w.Automerge = text("anything")
	}
	if g.chance(0.6) {
		w.PackConfig = map[string]any{"actionsMinuteRate": g.pick("0.008", "0", "-1", "x")}
		if v, err := strconv.ParseFloat(w.PackConfig["actionsMinuteRate"].(string), 64); err == nil {
			w.PackConfig["actionsMinuteRate"] = v
		}
	}
	if g.chance(0.1) {
		pr := 9
		w.Target.PR = &pr
	}
	switch {
	case g.chance(0.03):
		w.Target.Branch = "main"
	case g.chance(0.03):
		w.Target.Branch = ""
	}
	if g.chance(0.2) {
		w.Shallow = g.r.IntN(3) + 1
	}
	at := g.now
	for range g.r.IntN(4) {
		at = at.Add(time.Duration(g.r.IntN(40)) * time.Hour)
		if g.chance(0.15) {
			at = at.Add(-time.Duration(g.r.IntN(40)) * time.Hour)
		}
		step := usageStep{Now: isoMs(at), Unlanded: g.chance(0.2)}
		if w.Target.PR != nil && g.chance(0.5) {
			step = usageStep{Now: steps(w)[len(w.Steps)].Now, Unlanded: true}
		}
		if g.chance(0.3) {
			step.Mangle = g.r.Uint64()
		}
		w.Steps = append(w.Steps, step)
	}
	return w
}

func TestParityUsageFuzz(t *testing.T) {
	if os.Getenv(PacksTreeEnv) == "" {
		t.Skipf("the fuzz compares two live engines; %s names the ClaudinitePacks checkout", PacksTreeEnv)
	}
	n, seed := 40, 1
	if v, err := strconv.Atoi(os.Getenv(usageFuzzEnv)); err == nil {
		n = v
	}
	if v, err := strconv.Atoi(os.Getenv(usageFuzzSeedEnv)); err == nil {
		seed = v
	}
	decide := usageDecideBinary(t)
	for s := seed; s < seed+n; s++ {
		t.Run("seed-"+strconv.Itoa(s), func(t *testing.T) {
			r := rand.New(rand.NewPCG(uint64(s), 0))
			bases := []time.Time{time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC),
				time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)}
			base := bases[r.IntN(len(bases))]
			g := usageGen{r: r, now: base.Add(time.Duration(r.IntN(96*60)) * time.Minute)}
			w := g.world()
			want := asJSON(t, askUsage(t, "node", w, decide)).([]any)
			got := asJSON(t, askUsage(t, "cn", w, decide)).([]any)
			if dir := os.Getenv(usageFuzzDumpEnv); dir != "" {
				raw, _ := json.MarshalIndent(map[string]any{"input": w, "expect": want}, "", "  ")
				_ = os.WriteFile(filepath.Join(dir, "seed-"+strconv.Itoa(s)+".json"), raw, 0o644)
			}
			sameBytes(t, "cn", got, want)
			if !reflect.DeepEqual(got, want) {
				raw, _ := json.MarshalIndent(map[string]any{"input": w}, "", "  ")
				out := filepath.Join(os.TempDir(), "parity-usage-seed-"+strconv.Itoa(s)+".json")
				_ = os.WriteFile(out, raw, 0o644)
				t.Errorf("cn disagrees (world in %s):\n%s", out, strings.Join(jsonDiff("$", any(got), any(want), 30), "\n"))
			}
		})
	}
}

// steps are a world's runs, its first among them.
func steps(w usageWorld) []usageStep { return append([]usageStep{{Now: w.Now}}, w.Steps...) }
