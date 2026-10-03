package adopt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// Seed writes each of pack id's seedOps whose dest is absent, from the
// vendored tree, and returns the dests it wrote. A present dest is the
// member's own and is left alone; the pack update never seeds.
func Seed(repo, id string) ([]string, error) {
	dir := packset.Tree(repo, id)
	m, err := packset.ReadManifest(dir)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", id, err)
	}
	var wrote []string
	for _, op := range m.SeedOps {
		dest := filepath.Join(repo, filepath.FromSlash(op.Dest))
		if _, err := os.Lstat(dest); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return wrote, err
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(op.Template)))
		if err != nil {
			return wrote, fmt.Errorf("pack %s seeds %s from %s: %w", id, op.Dest, op.Template, err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return wrote, err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return wrote, err
		}
		wrote = append(wrote, op.Dest)
	}
	return wrote, nil
}

// seedAll seeds every pack in ids, printing one line per file written.
func seedAll(repo string, ids []string, out io.Writer) error {
	for _, id := range ids {
		wrote, err := Seed(repo, id)
		for _, d := range wrote {
			fmt.Fprintf(out, "seeded %s\n", d)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
