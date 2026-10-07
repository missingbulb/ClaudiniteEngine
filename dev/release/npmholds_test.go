package release

import (
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// docNPM serves each package's version document only after `after`
// requests for it, naming the integrity it holds for that package.
type docNPM struct {
	mu        sync.Mutex
	after     int
	seen      map[string]int
	integrity map[string]string
}

func (f *docNPM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pkg := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/1.61005.9")
	f.seen[pkg]++
	in, ok := f.integrity[pkg]
	if !ok || f.seen[pkg] <= f.after {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	fmt.Fprintf(w, `{"name":%q,"version":"1.61005.9","dist":{"integrity":%q}}`, pkg, in)
}

// holdsDist writes a staging dist of two tarballs and returns it with the
// integrity npm would name for each package.
func holdsDist(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "tarballs"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for pkg, file := range map[string]string{"@claudinite/cli": "cli-1.61005.9.tgz", "@claudinite/cli-linux-x64": "cli-linux-x64-1.61005.9.tgz"} {
		body := []byte("tarball of " + pkg)
		if err := os.WriteFile(filepath.Join(dir, "tarballs", file), body, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha512.Sum512(body)
		want[pkg] = "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.integrity"), []byte(manifestPin+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, want
}

// manifestPin is the dist's manifest.integrity, the pin from-npm.yml takes.
const manifestPin = "sha512-manifestpin"

func holdsInput(dist, registry string, log *bytes.Buffer) HoldsInput {
	return HoldsInput{Dist: dist, Version: "1.61005.9", Channel: "canary", Repo: "missingbulb/ClaudiniteEngine", Registry: registry, HTTP: http.DefaultClient,
		Timeout: 2 * time.Second, Every: time.Millisecond, Log: log}
}

func TestNPMHoldsWaitsForTheVersionDocument(t *testing.T) {
	t.Parallel()
	dist, want := holdsDist(t)
	f := &docNPM{after: 3, seen: map[string]int{}, integrity: want}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var log bytes.Buffer
	if err := NPMHolds(holdsInput(dist, srv.URL, &log)); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if f.seen["@claudinite/cli"] != 4 || f.seen["@claudinite/cli-linux-x64"] != 4 {
		t.Errorf("looked %v times, want 4 each", f.seen)
	}
	for _, p := range []string{"@claudinite/cli-linux-arm64", "@claudinite/cli-darwin-x64"} {
		if f.seen[p] != 0 {
			t.Errorf("looked for %s, which the dist does not hold", p)
		}
	}
	if !strings.Contains(log.String(), "npm-holds: @claudinite/cli 1.61005.9 matches") {
		t.Errorf("log does not record the match:\n%s", log.String())
	}
}

func TestNPMHoldsRefusesOtherBytes(t *testing.T) {
	t.Parallel()
	dist, want := holdsDist(t)
	want["@claudinite/cli-linux-x64"] = "sha512-" + strings.Repeat("A", 86) + "=="
	srv := httptest.NewServer(&docNPM{seen: map[string]int{}, integrity: want})
	defer srv.Close()
	var log bytes.Buffer
	err := NPMHolds(holdsInput(dist, srv.URL, &log))
	if err == nil || !strings.Contains(err.Error(), "@claudinite/cli-linux-x64") {
		t.Fatalf("err %v, want a refusal naming cli-linux-x64\n%s", err, log.String())
	}
}

// Publish has already put every package on npm when the wait runs out, so
// re-running it is refused; the error carries the one way on, from-npm.yml
// dispatched with this release's values, and names every package npm has
// not listed yet.
func TestNPMHoldsGivesUpWithTheWayOn(t *testing.T) {
	t.Parallel()
	dist, want := holdsDist(t)
	f := &docNPM{seen: map[string]int{}, integrity: map[string]string{"@claudinite/cli": want["@claudinite/cli"]}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var log bytes.Buffer
	in := holdsInput(dist, srv.URL, &log)
	in.Timeout = 50 * time.Millisecond
	err := NPMHolds(in)
	if err == nil {
		t.Fatalf("NPMHolds passed a registry that never lists cli-linux-x64\n%s", log.String())
	}
	msg := err.Error()
	for _, w := range []string{
		"does not list @claudinite/cli-linux-x64 1.61005.9",
		"gh workflow run from-npm.yml --repo missingbulb/ClaudiniteEngine --ref v1.61005.9 -f version=1.61005.9 -f integrity=" + manifestPin + " -f channel=canary",
	} {
		if !strings.Contains(msg, w) {
			t.Errorf("error lacks %q:\n%s", w, msg)
		}
	}
	if strings.Contains(msg, "@claudinite/cli 1.61005.9") {
		t.Errorf("error names cli, which npm listed:\n%s", msg)
	}
	if f.seen["@claudinite/cli"] != 1 {
		t.Errorf("looked at cli %d times; once it matched, want no more looks", f.seen["@claudinite/cli"])
	}
}

// Every package is looked at in each round, so the log records when each
// one landed and a slow package does not hide the state of those after it.
func TestNPMHoldsLooksAtEveryPackageEachRound(t *testing.T) {
	t.Parallel()
	dist, want := holdsDist(t)
	f := &docNPM{seen: map[string]int{}, integrity: map[string]string{"@claudinite/cli-linux-x64": want["@claudinite/cli-linux-x64"]}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var log bytes.Buffer
	in := holdsInput(dist, srv.URL, &log)
	in.Timeout = 50 * time.Millisecond
	err := NPMHolds(in)
	if err == nil || !strings.Contains(err.Error(), "does not list @claudinite/cli 1.61005.9") {
		t.Fatalf("err %v, want one naming cli\n%s", err, log.String())
	}
	if !strings.Contains(log.String(), "npm-holds: @claudinite/cli-linux-x64 1.61005.9 matches") {
		t.Errorf("linux-x64 was never looked at while cli was missing:\n%s", log.String())
	}
}

func TestNPMHoldsNeedsTheManifestPackage(t *testing.T) {
	t.Parallel()
	dist, want := holdsDist(t)
	if err := os.Remove(filepath.Join(dist, "tarballs", "cli-1.61005.9.tgz")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(&docNPM{seen: map[string]int{}, integrity: want})
	defer srv.Close()
	if err := NPMHolds(holdsInput(dist, srv.URL, &bytes.Buffer{})); err == nil {
		t.Fatal("NPMHolds passed a dist without the manifest package")
	}
}
