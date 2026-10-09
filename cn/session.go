package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/items"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/world"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/userpack"
)

// The session commands run inside a routine session, whose GitHub access
// is its own tools': they read what the session hands them in files and
// reach no network.

func readItemFile(path, repo string, issue int) (workitem.Issue, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		var item workitem.Issue
		if item, err = items.ReadItem(raw); err == nil {
			return item, nil
		}
	}
	return workitem.Issue{}, report.New(report.Usage, fmt.Sprintf("--item-file must hold the issue as your GitHub tools returned it (number, title, body, state, labels). Read it first: `issue_read` method `get` on %s #%d, save the JSON, pass the path", repo, issue))
}

func cmdWorkConverge(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("work converge", flag.ContinueOnError)
	issue := fs.Int("issue", 0, "")
	outcome := fs.String("outcome", "", "")
	summary := fs.String("summary", "", "")
	pr := fs.Int("pr", 0, "")
	repo := fs.String("repo", "", "")
	itemFile := fs.String("item-file", "", "")
	recordFailed := fs.String("record-failed", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *recordFailed != "" {
		if *issue <= 0 {
			return report.New(report.Usage, "--record-failed needs --issue N, the item it records")
		}
		// A convergence refused or never run still owes the census its
		// record.
		line, err := items.ExecRecord(*recordFailed, "#"+strconv.Itoa(*issue), "failed")
		if err != nil {
			return report.New(report.Usage, err.Error())
		}
		fmt.Fprintln(stdout, line)
		return nil
	}
	plan := items.Plan{Issue: *issue, Outcome: *outcome, Summary: *summary, PR: *pr}
	if err := items.CheckPlan(plan); err != nil {
		return report.New(report.Usage, err.Error())
	}
	if *repo == "" {
		return report.New(report.Usage, "--repo <owner/name> names the repository this item lives in")
	}
	item, err := readItemFile(*itemFile, *repo, *issue)
	if err != nil {
		return err
	}
	if no := items.Refusal(item, *issue); no != "" {
		return report.New(report.Verify, no)
	}
	fmt.Fprintln(stdout, "the transition below is yours to execute.\nMake these calls with your GitHub tools, in this order, changing nothing. Then stop.")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, items.SessionScript(item, plan, *repo))
	return nil
}

func cmdWorkValidate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("work validate", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	issue := fs.Int("issue", 0, "")
	nonce := fs.String("nonce", "", "")
	itemFile := fs.String("item-file", "", "")
	commentsFile := fs.String("comments-file", "", "")
	requestFile := fs.String("request-file", "", "")
	if err := flags(fs, args); err != nil {
		return err
	}
	if *issue <= 0 || *nonce == "" || *commentsFile == "" {
		return report.New(report.Usage, "work validate --issue N --nonce X --item-file PATH --comments-file PATH [--request-file PATH]")
	}
	item, err := readItemFile(*itemFile, "this repo", *issue)
	if err != nil {
		return err
	}
	if item.Number != *issue {
		return report.New(report.Verify, fmt.Sprintf("not this item's session: the item file holds #%d, not #%d", item.Number, *issue))
	}
	var comments []world.Comment
	raw, err := os.ReadFile(*commentsFile)
	if err == nil {
		err = json.Unmarshal(raw, &comments)
	}
	if err != nil {
		return report.New(report.Usage, "--comments-file must hold the item's comments as your GitHub tools returned them, oldest first (id, body)")
	}
	s := items.Session{Item: item, Comments: comments, Nonce: *nonce}
	if *requestFile != "" {
		req, err := readItemFile(*requestFile, "this repo", 0)
		if err != nil {
			return err
		}
		s.Request = &req
	}
	r, err := loadTaskRepo(*repo)
	if err != nil {
		return err
	}
	s.Tasks = r.tasks
	v, err := items.Validate(s)
	if err != nil {
		return report.New(report.Verify, err.Error())
	}
	fmt.Fprintf(stdout, "item #%s is this session's: %s\ntask file: %s\nmodel: %s\noutcome ceiling: %s\n",
		strconv.Itoa(item.Number), v.Task.Path(), v.TaskPath, v.Model, v.Outcome)
	if v.Outcome != taskspec.OutcomeNoPR {
		fmt.Fprintf(stdout, "delivery: %s\n", r.queue.Delivery)
	}
	if text := taskspec.EngineInstructions(v.Task); text != "" {
		fmt.Fprintf(stdout, "\nThe engine carries this task's file, which no checkout holds; run these instructions as the task file:\n\n%s", text)
	}
	return nil
}

// hookUserPack is SessionStart's user-pack step over the real process:
// its environment, the default HTTP client (which honours the session's
// proxy) and git.
type hookUserPack struct{}

func (hookUserPack) Prepare(repo string) string {
	r, ok := userpack.Prepare(repo, userpack.Env{Getenv: os.Getenv, Client: http.DefaultClient})
	if !ok {
		return ""
	}
	return r.Line()
}
