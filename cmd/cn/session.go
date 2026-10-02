package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/tasks/items"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
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
	if err := flags(fs, args); err != nil {
		return err
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

func cmdWorkRecordExec(args []string, stdout io.Writer) error {
	if len(args) != 3 {
		return report.New(report.Usage, "work record-exec <pack>/<task> <slot> <success|failed>")
	}
	line, err := items.ExecRecord(args[0], args[1], args[2])
	if err != nil {
		return report.New(report.Usage, err.Error())
	}
	fmt.Fprintln(stdout, line)
	return nil
}

func cmdWorkValidate(args []string, stdout io.Writer) error {
	roots, err := license.Roots()
	if err != nil {
		return report.Wrap(report.IO, "the embedded license roots", err)
	}
	return workValidate(args, stdout, func(grant string, issue int) error {
		return license.VerifyGrant([]byte(grant), roots, time.Now(), issue)
	})
}

func workValidate(args []string, stdout io.Writer, verify func(grant string, issue int) error) error {
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
	s := items.Session{Item: item, Comments: comments, Nonce: *nonce, VerifyGrant: verify, GrantOf: execute.GrantFromComment}
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
	return nil
}
