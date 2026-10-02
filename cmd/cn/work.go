package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/tasks/ghport"
	"github.com/missingbulb/ClaudiniteEngine/tasks/items"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

func cmdWork(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return report.New(report.Usage, "work takes create, wake, converge, record-exec or validate")
	}
	env := world.Env(os.Getenv)
	switch args[0] {
	case "create":
		return cmdWorkCreate(args[1:], stdout, env)
	case "wake":
		return cmdWorkWake(args[1:], stdout, env)
	case "converge":
		return cmdWorkConverge(args[1:], stdout)
	case "record-exec":
		return cmdWorkRecordExec(args[1:], stdout)
	case "validate":
		return cmdWorkValidate(args[1:], stdout)
	}
	return report.New(report.Usage, fmt.Sprintf("unknown work command %q", args[0]))
}

// issueRef reads "#12" or "12".
func issueRef(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if err != nil || n <= 0 {
		return 0, report.New(report.Usage, fmt.Sprintf("%q is not an issue number", s))
	}
	return n, nil
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func cmdWorkCreate(args []string, stdout io.Writer, env world.Env) error {
	fs := flag.NewFlagSet("work create", flag.ContinueOnError)
	repo := fs.String("repo", ".", "")
	urgent := fs.Bool("urgent", false, "")
	var context, blocked listFlag
	fs.Var(&context, "context", "")
	fs.Var(&blocked, "blocked-by", "")
	notBefore := fs.String("not-before", "", "")
	qualifier := fs.String("qualifier", "", "")
	supersedes := fs.String("supersedes", "", "")
	target, rest := "", args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		target, rest = rest[0], rest[1:]
	}
	if err := flags(fs, rest); err != nil {
		return err
	}
	if !strings.Contains(target, "/") {
		return report.New(report.Usage, "work create <pack>/<task> [--urgent] [--context TEXT] [--not-before ISO] [--blocked-by #N,#M] [--qualifier TEXT] [--supersedes #N]")
	}
	o := items.CreateOpts{Urgent: *urgent, Context: context, NotBefore: *notBefore, Qualifier: *qualifier}
	for _, b := range blocked {
		for _, part := range strings.Split(b, ",") {
			n, err := issueRef(part)
			if err != nil {
				return err
			}
			o.BlockedBy = append(o.BlockedBy, n)
		}
	}
	if *supersedes != "" {
		n, err := issueRef(*supersedes)
		if err != nil {
			return err
		}
		o.Supersedes = n
	}
	r, err := loadTaskRepo(*repo)
	if err != nil {
		return err
	}
	for _, t := range r.tasks {
		if t.Path() != target {
			continue
		}
		clock, err := env.Clock()
		if err != nil {
			return report.New(report.Usage, err.Error())
		}
		client, err := jobClient(env)
		if err != nil {
			return err
		}
		n, err := items.Create(ghport.New(client), t.Pack, t.ID, t.TaskPath(), t.Decl.IsScheduled(), o, clock.Now(),
			func(s string) { fmt.Fprintln(stdout, s) })
		if err != nil {
			return report.New(report.IO, err.Error())
		}
		suffix := ""
		if *urgent {
			suffix = " (urgent)"
		}
		fmt.Fprintf(stdout, "created #%d %s%s\n", n, target, suffix)
		return nil
	}
	return report.New(report.Verify, fmt.Sprintf("no task %q in this repo's declared packs", target))
}

func cmdWorkWake(args []string, stdout io.Writer, env world.Env) error {
	fs := flag.NewFlagSet("work wake", flag.ContinueOnError)
	urgent := fs.Bool("urgent", false, "")
	ref, rest := "", args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		ref, rest = rest[0], rest[1:]
	}
	if err := flags(fs, rest); err != nil {
		return err
	}
	n, err := issueRef(ref)
	if err != nil {
		return err
	}
	clock, err := env.Clock()
	if err != nil {
		return report.New(report.Usage, err.Error())
	}
	client, err := jobClient(env)
	if err != nil {
		return err
	}
	if err := items.Wake(ghport.New(client), n, *urgent, clock.Now()); err != nil {
		return report.New(report.IO, err.Error())
	}
	suffix := ""
	if *urgent {
		suffix = " (urgent)"
	}
	fmt.Fprintf(stdout, "woke #%d%s\n", n, suffix)
	return nil
}
