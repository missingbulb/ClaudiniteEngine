// Package workflows holds the three member workflows this engine version
// expects, embedded so the updater can tell when a member's copies differ
// from what a new release needs; the member fixture and cn init write them.
// The queue's two are the tasks capability's, served here beside the CI
// workflow.
// The nightly update workflow they superseded (the engine/update task runs
// the update now) stays embedded for the deprecation window, and a member
// still holding it is patched to delete it. The update writes no
// .github/workflows/ file itself: it stages what differs (Stage) for the
// update PR's agent stage to move.
package workflows

import (
	"embed"

	queue "github.com/missingbulb/ClaudiniteEngine/cn/tasks/workflows"
)

//go:embed templates/claudinite-update.yml templates/claudinite-ci.yml
var files embed.FS

// Names are the templates' file names under .github/workflows/.
var Names = append([]string{"claudinite-ci.yml"}, queue.Names...)

// Superseded is the nightly update workflow, which the engine/update task
// replaces wherever the queue runs.
const Superseded = "claudinite-update.yml"

// SupersededTemplate is the update workflow as the engine last wrote it.
func SupersededTemplate() []byte {
	raw, err := files.ReadFile("templates/" + Superseded)
	if err != nil {
		panic(err)
	}
	return raw
}

// CronPlaceholder is the scheduler template's cron, which cn init rewrites
// to the repo's hashed minute.
const CronPlaceholder = queue.CronPlaceholder

// SecretsMarker is the executor template's line beneath which cn init
// writes each declared task secret.
const SecretsMarker = queue.SecretsMarker

// Templates maps each file name to its content.
func Templates() map[string][]byte {
	out := map[string][]byte{}
	raw, err := files.ReadFile("templates/claudinite-ci.yml")
	if err != nil {
		panic(err)
	}
	out["claudinite-ci.yml"] = raw
	for _, n := range queue.Names {
		out[n] = queue.Template(n)
	}
	return out
}
