// Package prune is the retention on the conversation-logs branch: which
// captures are past the repo's window, read off their names, and the one
// commit that removes them.
package prune

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/growth/capture"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/gitcmd"
)

// Plan is one prune: the captures to delete, how many the branch holds,
// and Off when no window applies.
type Plan struct {
	Delete   []string `json:"delete"`
	LogCount int      `json:"logCount"`
	Off      bool     `json:"off"`
}

// PlanPrune plans a prune over the branch's flat listing at now, against
// a resolved window (nil is off). Age is the whole test, and the
// capture's name carries it; a file that is not a capture is never
// touched.
func PlanPrune(names []string, retention *float64, now time.Time) Plan {
	p := Plan{Delete: []string{}, Off: retention == nil || *retention <= 0}
	for _, n := range names {
		parsed, ok := capture.ParseLogFilename(n)
		if !ok {
			continue
		}
		p.LogCount++
		if p.Off {
			continue
		}
		at, err := time.Parse(time.RFC3339, parsed.CapturedAt)
		if err != nil {
			continue
		}
		if float64(now.Sub(at).Milliseconds())/86400000 > *retention {
			p.Delete = append(p.Delete, n)
		}
	}
	return p
}

// Attempts is how many pushes a prune tries, re-planning after each lost
// race with a capture.
const Attempts = 3

// Run prunes the branch on origin from the checkout at root, saying what
// it did on out. The checkout, its index and the branch's history are
// never touched: the removals are one commit on the fetched tip.
func Run(git gitcmd.Repo, branch string, retention float64, now func() time.Time, out io.Writer) error {
	days := jsjson.FormatNumber(retention)
	ls, err := git.Run("ls-remote", "--heads", "origin", branch)
	if err != nil {
		return err
	}
	if ls.Code != 0 {
		return fmt.Errorf("could not reach origin: %s", strings.TrimSpace(ls.Stderr))
	}
	if strings.TrimSpace(ls.Stdout) == "" {
		fmt.Fprintf(out, "no %s branch — nothing captured yet\n", branch)
		return nil
	}
	for attempt := 1; ; attempt++ {
		if err := ok(git.Run("fetch", "--quiet", "origin", branch)); err != nil {
			return err
		}
		tip, err := line(git, "rev-parse", "FETCH_HEAD")
		if err != nil {
			return err
		}
		tree, err := git.Run("ls-tree", tip)
		if err != nil {
			return err
		}
		var lines, names []string
		for _, l := range strings.Split(tree.Stdout, "\n") {
			if l == "" {
				continue
			}
			_, name, _ := strings.Cut(l, "\t")
			lines, names = append(lines, l), append(names, name)
		}
		r := retention
		plan := PlanPrune(names, &r, now())
		if len(plan.Delete) == 0 {
			fmt.Fprintf(out, "%d capture(s) on the branch, none past %sd\n", plan.LogCount, days)
			return nil
		}
		gone := map[string]bool{}
		for _, d := range plan.Delete {
			gone[d] = true
		}
		var kept []string
		for i, l := range lines {
			if !gone[names[i]] {
				kept = append(kept, l)
			}
		}
		input := strings.Join(kept, "\n")
		if input != "" {
			input += "\n"
		}
		newTree, err := git.RunInput(input, "mktree")
		if err := ok(newTree, err); err != nil {
			return err
		}
		msg := fmt.Sprintf("Claudinite: prune %d conversation log(s) past %sd retention [skip ci]", len(plan.Delete), days)
		commit, err := line(git, "-c", "user.name=claudinite[bot]", "-c", "user.email=claudinite@users.noreply.github.com",
			"commit-tree", strings.TrimSpace(newTree.Stdout), "-p", tip, "-m", msg)
		if err != nil {
			return err
		}
		push, err := git.Run("push", "--quiet", "origin", commit+":refs/heads/"+branch)
		if err != nil {
			return err
		}
		if push.Code != 0 {
			if attempt >= Attempts {
				return errors.New("push rejected: " + strings.TrimSpace(push.Stderr))
			}
			fmt.Fprintf(out, "push rejected (attempt %d/%d) — re-planning against the new tip\n", attempt, Attempts)
			continue
		}
		fmt.Fprintf(out, "pruned %d of %d capture(s), past %sd\n", len(plan.Delete), plan.LogCount, days)
		return nil
	}
}

func ok(r gitcmd.Ran, err error) error {
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return errors.New(strings.TrimSpace(r.Stderr))
	}
	return nil
}

func line(git gitcmd.Repo, args ...string) (string, error) {
	r, err := git.Run(args...)
	if err := ok(r, err); err != nil {
		return "", err
	}
	return strings.TrimSpace(r.Stdout), nil
}
