// Package workflows holds the two member workflows this engine version
// expects, embedded so the updater can tell when a member's copies differ
// from what a new release needs; the member fixture and cn init write them.
// No update writes .github/workflows/.
package workflows

import "embed"

//go:embed templates/claudinite-update.yml templates/claudinite-ci.yml
var files embed.FS

// Names are the templates' file names under .github/workflows/.
var Names = []string{"claudinite-update.yml", "claudinite-ci.yml"}

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
