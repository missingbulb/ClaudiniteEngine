package update

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/land"
)

// updateMerger is the landing lane's merge over the updater's clients.
type updateMerger struct{ d Deps }

func (m updateMerger) MergePull(in land.Merge) error {
	return m.d.GitHub.MergePull(in.Number, in.SHA, in.Title)
}

func (m updateMerger) DeleteBranch(ref string) error {
	return m.d.Git.DeleteRemoteBranch(remote, ref)
}

// landPinned enters the landing lane at its merge, every landing's one
// way in: it waits for every workflow's verdict on sha (awaitHead) and,
// when one failed or is still running at the bound, merges nothing and
// says why; otherwise only the pinned-sha merge and the branch's removal
// remain.
func landPinned(d Deps, pr githubapi.PR, sha, title string) (why string, err error) {
	if why, err := awaitHead(d, sha); why != "" || err != nil {
		return why, err
	}
	err, tidy := land.Pinned(updateMerger{d}, land.PR{Number: pr.Number, HeadRef: pr.HeadRef, HeadSHA: sha}, title, "", nil)
	if err != nil {
		return "", err
	}
	return "", tidy
}

// notLanded is Land's verdict on update PR n left unmerged for why.
func notLanded(n int, why string) string {
	return fmt.Sprintf("skipped: #%d not landed: %s", n, why)
}
