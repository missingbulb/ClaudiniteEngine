package update

import (
	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/tasks/land"
)

// updateMerger is the landing lane's merge over the updater's clients.
type updateMerger struct{ d Deps }

func (m updateMerger) MergePull(in land.Merge) error {
	return m.d.GitHub.MergePull(in.Number, in.SHA, in.Title)
}

func (m updateMerger) DeleteBranch(ref string) error {
	return m.d.Git.DeleteRemoteBranch(remote, ref)
}

// landPinned enters the landing lane at its merge: an update PR is landed
// on the CI run its updater dispatched, so the evidence is in hand and
// only the pinned-sha merge and the branch's removal remain.
func landPinned(d Deps, pr githubapi.PR, sha, title string) error {
	err, tidy := land.Pinned(updateMerger{d}, land.PR{Number: pr.Number, HeadRef: pr.HeadRef, HeadSHA: sha}, title, "", nil)
	if err != nil {
		return err
	}
	return tidy
}
