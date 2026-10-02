// Package workflows holds the three member workflows this engine version
// expects, embedded so the updater can tell when a member's copies differ
// from what a new release needs; the member fixture and cn init write them.
// The nightly update workflow they superseded (the engine/update task runs
// the update now) stays embedded for the deprecation window, and a member
// still holding it is patched to delete it. No update writes
// .github/workflows/.
package workflows

import "embed"

//go:embed templates/claudinite-update.yml templates/claudinite-ci.yml templates/claudinite-scheduler.yml templates/claudinite-executor.yml
var files embed.FS

// Names are the templates' file names under .github/workflows/.
var Names = []string{"claudinite-ci.yml", "claudinite-scheduler.yml", "claudinite-executor.yml"}

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
const CronPlaceholder = "10 4,16 * * *"

// SecretsMarker is the executor template's line beneath which cn init
// writes each declared task secret.
const SecretsMarker = "# claudinite:secrets"

// Templates maps each file name to its content.
func Templates() map[string][]byte {
	out := map[string][]byte{}
	for _, n := range Names {
		raw, err := files.ReadFile("templates/" + n)
		if err != nil {
			panic(err)
		}
		out[n] = raw
	}
	return out
}
