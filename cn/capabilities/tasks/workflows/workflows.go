// Package workflows holds the queue's two member workflows, the scheduler
// and the executor, as this engine version expects them. lifecycle's
// workflows package serves them with the rest of a member's workflows.
package workflows

import "embed"

//go:embed templates/claudinite-scheduler.yml templates/claudinite-executor.yml
var files embed.FS

// Names are the templates' file names under .github/workflows/.
var Names = []string{"claudinite-scheduler.yml", "claudinite-executor.yml"}

// CronPlaceholder is the scheduler template's cron, which cn adopt rewrites
// to the repo's hashed minute.
const CronPlaceholder = "10 4,16 * * *"

// SecretsMarker is the executor template's line beneath which cn adopt
// writes each declared task secret.
const SecretsMarker = "# claudinite:secrets"

// Template is the named template's content.
func Template(name string) []byte {
	raw, err := files.ReadFile("templates/" + name)
	if err != nil {
		panic(err)
	}
	return raw
}
