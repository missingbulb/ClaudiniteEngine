// Command checkprog stands in for a pack's compiled checks in the timing
// probe: it walks a tree, matches lines and prints findings as JSON.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type finding struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Rule string `json:"rule"`
}

var todo = regexp.MustCompile(`\bTODO\b|\bFIXME\b`)

func main() {
	var out []finding
	_ = filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		s := bufio.NewScanner(f)
		for n := 1; s.Scan(); n++ {
			if todo.MatchString(s.Text()) {
				out = append(out, finding{p, n, "no-todo"})
			}
		}
		return nil
	})
	_ = json.NewEncoder(os.Stdout).Encode(out)
}
