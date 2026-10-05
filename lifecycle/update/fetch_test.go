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
