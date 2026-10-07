// Command derive times, in process, the derivation every per-call hook
// makes from the tree before it can judge a call: the active packs and
// their skills' triggers, the declared checks, and the checks binary's
// key, each run memoized afresh as a hook process is. It prints one
// sample in milliseconds per line, for timeit samples.
//
//	derive --repo DIR --runs N
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks"
	"github.com/missingbulb/ClaudiniteEngine/cn/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/skilltriggers"
)

// engine is the version the probe's member pins, which loads every pack.
const engine = "0.0.0"

func main() {
	repo := flag.String("repo", "", "")
	runs := flag.Int("runs", 0, "")
	flag.Parse()
	if err := run(*repo, *runs); err != nil {
		fmt.Fprintln(os.Stderr, "derive:", err)
		os.Exit(1)
	}
}

func run(repo string, runs int) error {
	if repo == "" || runs <= 0 {
		return errors.New("usage: derive --repo DIR --runs N")
	}
	svc := checks.Service{Build: build.Config{Engine: engine}}
	for i := 0; i < runs; i++ {
		packset.Forget()
		packset.Memoize()
		start := time.Now()
		set, err := packset.Load(repo, engine, true)
		if err != nil {
			return err
		}
		ts, _ := skilltriggers.FromPacks(set.Packs)
		ds, err := declared.LoadSet(repo, engine)
		if err != nil {
			return err
		}
		if _, _, err := svc.Key(repo); err != nil {
			return err
		}
		if i == 0 {
			fmt.Fprintf(os.Stderr, "derive: %d packs, %d triggers, %d declared checks\n", len(set.Packs), len(ts), len(ds.Checks))
		}
		fmt.Printf("%.3f\n", float64(time.Since(start).Microseconds())/1000)
	}
	return nil
}
