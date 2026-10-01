package lifecycle

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/paths"
	"github.com/missingbulb/ClaudiniteEngine/shared/report"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// SelftestInput is what the caller gathers for cn selftest, so lifecycle
// needs no other capability.
type SelftestInput struct {
	CacheRoot string
	RootIDs   []string
	RootsErr  error
	Now       time.Time
}

// Selftest prints what this machine needs to run the engine and returns 0
// when the cache is writable and the embedded roots parse, else 1.
func Selftest(w io.Writer, in SelftestInput) int {
	code := 0
	fmt.Fprintf(w, "version %s\nplatform %s\n", version.Version(), version.Platform())
	if err := writable(in.CacheRoot); err != nil {
		fmt.Fprintf(w, "cache %s not writable: %v\n", in.CacheRoot, err)
		code = 1
	} else {
		fmt.Fprintf(w, "cache %s writable\n", in.CacheRoot)
	}
	if in.RootsErr != nil {
		fmt.Fprintf(w, "roots invalid: %v\n", in.RootsErr)
		code = 1
	} else {
		fmt.Fprintf(w, "roots %s\n", strings.Join(in.RootIDs, " "))
	}
	fmt.Fprintf(w, "crashes (7 days) %d\n", report.CountRecent(report.CrashDir(in.CacheRoot), in.Now, 7*24*time.Hour))
	return code
}

func writable(dir string) error {
	if err := paths.EnsurePrivateDir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".selftest-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
