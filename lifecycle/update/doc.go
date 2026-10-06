// Package update is cn update engine and cn update land: it moves a
// member's engine pin by pull request, one action per run.
//
// A run reads main's claudinite-ci.yml runs first and does nothing more
// while main is not green, before any npm read. A green open update PR is
// landed and the run stops. Otherwise it picks the newest version of the
// member's channel package that is newer than the pin and neither held,
// revoked nor deprecated, downloads and verifies it exactly as the
// launcher would, placing it in the launcher's cache, runs its selftest
// and its verify against this repo, and opens a PR moving the pin on
// claudinite/engine-<version>, superseding an older open update PR. The
// workflows the new binary expects of this repo (`cn workflows stage`) ride
// that PR staged under .claudinite/cache/pending-workflows/, since the job
// token may not push .github/workflows/; the engine/update task's agent
// stage moves them into place, and land accepts there exactly what the
// pinned engine expects.
//
// The GitHub calls, all with the workflow job's GITHUB_TOKEN: list
// workflow runs for a head sha, list open PRs, get one PR, create, close
// and squash-merge a PR (the merge pinned to the head sha CI ran on), add
// a label, comment, dispatch a workflow, and list, create, update and
// close issues. Branches are pushed and deleted with git, on the credentials
// the checkout configured.
//
// Events the job token causes start no workflow runs, except
// workflow_dispatch; a pull_request it opens gets runs that wait for
// approval
// (https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow).
// So the updater dispatches claudinite-ci.yml on the update branch and
// lands only on a workflow_dispatch run's verdict, dispatches it on main
// after a merge, and ignores action_required runs when judging main.
package update
