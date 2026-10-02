// Package workflows holds the four member workflows this engine version
// expects, embedded so the updater can tell when a member's copies differ
// from what a new release needs; the member fixture and cn init write them.
// No update writes .github/workflows/.
package workflows

import "embed"

//go:embed templates/claudinite-update.yml templates/claudinite-ci.yml templates/claudinite-scheduler.yml templates/claudinite-executor.yml
var files embed.FS

// Names are the templates' file names under .github/workflows/.
var Names = []string{"claudinite-update.yml", "claudinite-ci.yml", "claudinite-scheduler.yml", "claudinite-executor.yml"}

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
