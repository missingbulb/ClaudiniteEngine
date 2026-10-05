package update

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const pkg = "@claudinite/cli"

func fetchIn(t *testing.T, r *registry, ver string) FetchInput {
	t.Helper()
	p, err := r.client().Packument(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return FetchInput{Registry: r.client(), Package: pkg, Version: ver, Packument: p, Roots: rootsOf(testRoot), CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Platform: version.Platform(), Now: t0}
}

func TestFetchPlacesTheVerifiedBinaryWhereTheLauncherLooks(t *testing.T) {
	t.Parallel()
	r := newRegistry(t)
	r.publish(t, pkg, "1.60930.2", relOpts{})
	in := fetchIn(t, r, "1.60930.2")
	got, err := Fetch(in)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(in.CacheRoot, "1.60930.2")
	if got.Binary != filepath.Join(dir, binaryName(version.Platform())) {
		t.Errorf("binary at %s", got.Binary)
	}
	st, err := os.Stat(got.Binary)
	if err != nil || st.Mode().Perm() != 0o555 {
		t.Errorf("binary mode %v %v", st.Mode(), err)
	}
	mst, err := os.Stat(filepath.Join(dir, "manifest.json"))
	if err != nil || mst.Mode().Perm() != 0o444 {
		t.Errorf("manifest mode %v %v", mst.Mode(), err)
	}
	if dst, _ := os.Stat(dir); dst.Mode().Perm() != 0o700 {
		t.Errorf("cache dir mode %v", dst.Mode())
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if got.Integrity != integrity(raw) || !strings.HasPrefix(got.Integrity, "sha512-") {
		t.Errorf("integrity %s", got.Integrity)
	}
	if got.KeyID != sign.KeyID(relKey.Public().(ed25519.PublicKey)) {
		t.Errorf("key id %s", got.KeyID)
	}
	if string(got.Launcher) != "#!/bin/sh\n# launcher of 1.60930.2\n" {
		t.Errorf("launcher %q", got.Launcher)
	}
	if sst, err := os.Stat(filepath.Join(dir, "manifest.sig.json")); err != nil || sst.Mode().Perm() != 0o444 {
		t.Errorf("signature not cached: %v", err)
	}
}

func TestFetchRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		o    relOpts
		in   func(*FetchInput)
		want string
	}{
		{"flipped channel tarball", relOpts{flipChannel: true}, nil, "integrity"},
		{"flipped binary byte", relOpts{flipBinary: true}, nil, "does not match its manifest entry"},
		{"packs certificate", relOpts{use: sign.UsePacks}, nil, "use"},
		{"expired certificate", relOpts{notBefore: t0.AddDate(0, 0, -60)}, nil, "expired"},
		{"root the binary does not embed", relOpts{issuer: otherRoot}, nil, "trusted root"},
		{"not in the packument", relOpts{}, func(in *FetchInput) { in.Version = "1.60930.9" }, "no 1.60930.9"},
		{"over the size cap", relOpts{}, func(in *FetchInput) { in.Registry.MaxBytes = 10 }, "cap"},
	}
	for _, c := range cases {
		r := newRegistry(t)
		r.publish(t, pkg, "1.60930.2", c.o)
		in := fetchIn(t, r, "1.60930.2")
		if c.in != nil {
			c.in(&in)
		}
		_, err := Fetch(in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
			continue
		}
		left, _ := filepath.Glob(filepath.Join(in.CacheRoot, "*", "*"))
		if len(left) != 0 {
			t.Errorf("%s: cache holds %v after a refusal", c.name, left)
		}
	}
}

// mirrorOnly moves ver's tarballs from their registry paths to the mirror's
// /v<ver>/<file>, as in the minutes after a publish when npm lists a
// version but answers 404 for its tarballs, and points in at the mirror.
func mirrorOnly(t *testing.T, r *registry, ver string, in *FetchInput) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for p, b := range r.files {
		if strings.HasSuffix(p, "-"+ver+".tgz") {
			delete(r.files, p)
			r.files["/v"+ver+"/"+p[strings.LastIndex(p, "/")+1:]] = b
		}
	}
	in.Registry.Mirror = r.srv.URL
}

func TestFetchFallsBackToTheMirror(t *testing.T) {
	t.Parallel()
	r := newRegistry(t)
	r.publish(t, pkg, "1.60930.2", relOpts{})
	in := fetchIn(t, r, "1.60930.2")
	mirrorOnly(t, r, "1.60930.2", &in)
	got, err := Fetch(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got.Binary); err != nil {
		t.Fatal(err)
	}
	var fromMirror int
	for _, p := range r.requests() {
		if strings.HasPrefix(p, "/v1.60930.2/") {
			fromMirror++
		}
	}
	if fromMirror != 2 {
		t.Errorf("read %d tarballs from the mirror, want 2: %v", fromMirror, r.requests())
	}
}

func TestFetchRefusesWhatTheMirrorServes(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		o    relOpts
		want string
	}{
		"flipped channel tarball": {relOpts{flipChannel: true}, "integrity"},
		"flipped binary byte":     {relOpts{flipBinary: true}, "does not match its manifest entry"},
	} {
		r := newRegistry(t)
		r.publish(t, pkg, "1.60930.2", c.o)
		in := fetchIn(t, r, "1.60930.2")
		mirrorOnly(t, r, "1.60930.2", &in)
		if _, err := Fetch(in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	// With no mirror, a tarball npm does not serve yet is a download error.
	r := newRegistry(t)
	r.publish(t, pkg, "1.60930.2", relOpts{})
	in := fetchIn(t, r, "1.60930.2")
	mirrorOnly(t, r, "1.60930.2", &in)
	in.Registry.Mirror = ""
	if _, err := Fetch(in); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("no mirror: %v, want the registry's 404", err)
	}
}
