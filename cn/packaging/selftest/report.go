// Package selftest is cn selftest: whether this binary can run on this
// machine and, given --repo, on that member as it stands. The updater runs
// a candidate's selftest over the member before it opens a pull request,
// and a failed probe stops the update there.
//
// The first line is "version <v>", which every updater reads; each further
// line is one probe, "<ok|skip|fail> <probe>[: <detail>]", where skip means
// the member's shape makes the probe inapplicable. Any fail exits 1.
package selftest

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/paths"
)

// Status is a probe's answer.
type Status string

const (
	OK   Status = "ok"
	Skip Status = "skip"
	Fail Status = "fail"
)

// Probe is one line of the report.
type Probe struct {
	Name   string
	Status Status
	Detail string
}

// Input is what the caller gathers for cn selftest, so selftest needs no
// other capability.
type Input struct {
	Version   string
	Platform  string
	CacheRoot string
	RootIDs   []string
	RootsErr  error
	Now       time.Time
	// Repo is the member to probe; "" runs the machine probes alone.
	Repo string
	// HookEvents are the events this binary's cn hook answers.
	HookEvents []string
}

// Selftest prints the report and returns 1 when a probe failed, else 0.
func Selftest(w io.Writer, in Input) int {
	fmt.Fprintf(w, "version %s\n", in.Version)
	code := 0
	for _, p := range Run(in) {
		line := string(p.Status) + " " + p.Name
		if p.Detail != "" {
			line += ": " + p.Detail
		}
		fmt.Fprintln(w, line)
		if p.Status == Fail {
			code = 1
		}
	}
	return code
}

var failLine = regexp.MustCompile(`(?m)^fail ([a-z]+)\b`)

// Failed names the probes a report failed, in report order.
func Failed(report string) []string {
	var out []string
	for _, m := range failLine.FindAllStringSubmatch(report, -1) {
		out = append(out, m[1])
	}
	return out
}

func writable(dir string) error {
	if err := paths.EnsurePrivateDir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".selftest-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
