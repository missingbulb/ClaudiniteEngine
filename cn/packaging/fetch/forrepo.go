package fetch

import (
	"crypto/ed25519"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// ForRepo reads the pack indexes from the pack sources repo's settings
// name, logging which answered to out. close removes every branch's clone.
func ForRepo(repo string, roots []ed25519.PublicKey, out io.Writer) (*Reader, func(), error) {
	list, err := repoSources(repo)
	if err != nil {
		return nil, nil, err
	}
	srcs, closer := SourcesFor(list, &http.Client{Timeout: time.Minute})
	return &Reader{Sources: srcs, Roots: roots, Now: time.Now, Log: out}, closer, nil
}

// repoSources is packs.sources from repo's settings: none where the repo
// holds no settings file yet, an adoption, which reads the shelf.
func repoSources(repo string) ([]string, error) {
	held := false
	for _, f := range settings.Formats {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(settings.RelPath(f)))); err == nil {
			held = true
		}
	}
	if !held {
		return nil, nil
	}
	path, f, err := settings.Find(repo)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return nil, err
	}
	return p.Sources, nil
}
