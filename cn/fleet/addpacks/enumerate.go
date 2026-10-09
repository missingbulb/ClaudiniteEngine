package addpacks

import (
	"errors"
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
)

// Enumerate is the owner's repositories; none refuses the sweep, which
// would otherwise read an empty fleet as a converged one.
func Enumerate(gh fleet.GH, owner string) ([]fleet.Repo, error) {
	repos, err := fleet.Enumerate(gh, owner)
	if errors.Is(err, fleet.ErrNoOwnedRepos) {
		return nil, fmt.Errorf("%w; refusing to run a sweep that would read an empty fleet as a converged one", err)
	}
	return repos, err
}
