package launcher

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// npxLayout lays the channel package out as npx does: the package folder
// under node_modules, its bin linked from node_modules/.bin. It returns
// the bin's path.
func npxLayout(t *testing.T, pkg string, files map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "node_modules", filepath.FromSlash(pkg))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(repoRoot, "launcher", "launch"))
	if err != nil {
		t.Fatal(err)
	}
	files["launch"] = src
	files["package.json"] = []byte("{\n  \"name\": \"" + pkg + "\",\n  \"version\": \"" + testVersion + "\",\n  \"bin\": {\"cn\": \"launch\"}\n}\n")
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "node_modules", ".bin")
	_ = os.MkdirAll(bin, 0o755)
	if err := os.Symlink(filepath.Join("..", filepath.FromSlash(pkg), "launch"), filepath.Join(bin, "cn")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(bin, "cn")
}

// releaseSig is the development release key's signature over manifest.
func releaseSig(t *testing.T, manifest []byte) []byte {
	t.Helper()
	key, err := sign.ParsePrivateKey(readFile(t, filepath.Join(repoRoot, "keys", "dev", "release.key")))
	if err != nil {
		t.Fatal(err)
	}
	var cert sign.Certificate
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(repoRoot, "keys", "dev", "release.cert.json"))), &cert); err != nil {
		t.Fatal(err)
	}
	sig, _ := json.Marshal(sign.SignManifest(key, cert, manifest))
	return sig
}

func runBin(t *testing.T, m *member, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = m.dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + m.home, "XDG_CACHE_HOME=" + m.cache,
		"CLAUDINITE_REGISTRY=" + m.registry, "CURL_CA_BUNDLE=" + m.ca, "SSL_CERT_FILE=" + m.ca, "NO_PROXY=127.0.0.1,localhost",
		"CLAUDINITE_PACKS_CDN=https://127.0.0.1:1", "CLAUDINITE_PACKS_REPO=" + filepath.Join(m.home, "no-such-repo")}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

// The npx line reaches cn init from the signed manifest npx unpacked: the
// launcher fetches and checks the binary, caches it with the manifest and
// its signature, and cn init runs (here it stops at the pack sources,
// which are unreachable, before writing anything).
func TestBootstrapRunsCnInitFromTheNpxPackage(t *testing.T) {
	const pkg = "@claudinite/cli-rc"
	r := makeRelease(t, releaseOpts{pkg: pkg})
	sig := releaseSig(t, r.manifest)
	src, _ := os.ReadFile(filepath.Join(repoRoot, "launcher", "launch"))
	err := releasefiles.WriteTarball(filepath.Join(r.dist, "tarballs", "cli-rc-"+testVersion+".tgz"), []releasefiles.TarFile{
		{Name: "package.json", Mode: 0o644, Data: []byte("{}\n")},
		{Name: "manifest.json", Mode: 0o644, Data: r.manifest},
		{Name: "manifest.sig.json", Mode: 0o644, Data: sig},
		{Name: "launch", Mode: 0o755, Data: src},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := startStub(t, r.dist)
	m := newMember(t, s)
	_ = os.RemoveAll(filepath.Join(m.dir, ".claudinite"))
	bin := npxLayout(t, pkg, map[string][]byte{"manifest.json": r.manifest, "manifest.sig.json": sig})

	out, errOut, code := runBin(t, m, bin, "init", "--packs", "hello", "--channel", "canary", "--package", pkg, "--repo", m.dir)
	if code != 1 || !strings.Contains(errOut, "hello") || !strings.Contains(errOut, "no source answered") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "parameter not set") {
		t.Errorf("the launcher read an unset variable:\n%s", errOut)
	}
	for _, f := range []string{hostBin, "manifest.json", "manifest.sig.json"} {
		if _, err := os.Stat(filepath.Join(m.vdir(), f)); err != nil {
			t.Errorf("not cached: %s", f)
		}
	}
	if entries, _ := os.ReadDir(m.dir); len(entries) != 0 {
		t.Errorf("init wrote into the repo before every read succeeded: %v", entries)
	}

	// An unsigned package never reaches cn.
	m2 := newMember(t, s)
	_ = os.RemoveAll(filepath.Join(m2.dir, ".claudinite"))
	bin = npxLayout(t, pkg, map[string][]byte{"manifest.json": r.manifest})
	if _, errOut, code := runBin(t, m2, bin, "init", "--packs", "hello", "--repo", m2.dir); code != 1 || !strings.Contains(errOut, "unsigned manifest") {
		t.Errorf("unsigned: exit %d %s", code, errOut)
	}
	m2.cachedNothing(t)

	// A signature that does not verify stops cn init itself.
	m3 := newMember(t, s)
	_ = os.RemoveAll(filepath.Join(m3.dir, ".claudinite"))
	bad := bytes.Replace(sig, []byte(`"signature":"`), []byte(`"signature":"A`), 1)
	bin = npxLayout(t, pkg, map[string][]byte{"manifest.json": r.manifest, "manifest.sig.json": bad})
	if _, errOut, code := runBin(t, m3, bin, "init", "--packs", "hello", "--repo", m3.dir); code != 1 || !strings.Contains(errOut, "manifest signature") {
		t.Errorf("bad signature: exit %d %s", code, errOut)
	}
}
