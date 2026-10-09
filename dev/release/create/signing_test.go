package create

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/dev/test/scripttest"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
)

func sumsCheck(dist string) (string, error) {
	cmd := exec.Command("sha256sum", "-c", "--quiet", filepath.Join(filepath.Base(dist), "SHA256SUMS"))
	cmd.Dir = filepath.Dir(dist)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func TestBuildWritesAnUnsignedManifestAndSums(t *testing.T) {
	t.Parallel()
	dist, out := scripttest.UnsignedDist(t)
	if _, err := os.Stat(filepath.Join(dist, "manifest.sig.json")); err == nil {
		t.Error("build.sh wrote manifest.sig.json")
	}
	if _, err := os.Stat(filepath.Join(dist, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.SplitN(line, "  ", 2)
		if len(f) != 2 || len(f[0]) != 64 {
			t.Fatalf("SHA256SUMS line %q", line)
		}
		listed[f[1]] = true
	}
	var want []string
	for _, sub := range []string{"bin", "npm", "tarballs"} {
		_ = filepath.Walk(filepath.Join(dist, sub), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(filepath.Dir(dist), p)
				want = append(want, rel)
			}
			return nil
		})
	}
	want = append(want, "dist/manifest.json")
	for _, w := range want {
		if !listed[w] {
			t.Errorf("SHA256SUMS does not list %s", w)
		}
	}
	if len(listed) != len(want) {
		t.Errorf("SHA256SUMS lists %d files, want %d", len(listed), len(want))
	}
	if o, err := sumsCheck(dist); err != nil {
		t.Errorf("sha256sum -c: %v %s", err, o)
	}
	if o, err := scripttest.Run(t, []string{"DIST=" + dist}, "dev/release/verify/smoke.sh"); err != nil {
		t.Errorf("smoke.sh on an unsigned dist: %v\n%s", err, o)
	}
	if !strings.HasPrefix(lastLine(out), "manifest sha512-") {
		t.Errorf("build.sh's last line %q is not the integrity", lastLine(out))
	}
}

func TestSignSignsCopiesAndRepacks(t *testing.T) {
	t.Parallel()
	dist, buildOut := scripttest.UnsignedDist(t)
	out, err := scripttest.Run(t, append(scripttest.DevKeyEnv(t), "DIST="+dist), "dev/release/create/sign.sh")
	if err != nil {
		t.Fatalf("sign.sh: %v\n%s", err, out)
	}
	sig, err := os.ReadFile(filepath.Join(dist, "manifest.sig.json"))
	if err != nil {
		t.Fatal(err)
	}
	inPkg, err := os.ReadFile(filepath.Join(dist, "npm/cli/package/manifest.sig.json"))
	if err != nil || string(inPkg) != string(sig) {
		t.Fatalf("npm package signature: %v", err)
	}
	listing, err := exec.Command("tar", "-tzf", filepath.Join(dist, "tarballs/cli-1.61001.1.tgz")).CombinedOutput()
	if err != nil || !strings.Contains(string(listing), "package/manifest.sig.json") {
		t.Fatalf("tarball lacks the signature: %v %s", err, listing)
	}
	if lastLine(out) != lastLine(buildOut) {
		t.Errorf("sign.sh printed %q, build.sh %q", lastLine(out), lastLine(buildOut))
	}
	if o, err := sumsCheck(dist); err != nil {
		t.Fatalf("sha256sum -c after signing: %v %s", err, o)
	}
	sums, _ := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	for _, f := range []string{"dist/manifest.sig.json", "dist/npm/cli/package/manifest.sig.json"} {
		if !strings.Contains(string(sums), "  "+f+"\n") {
			t.Errorf("SHA256SUMS does not list %s", f)
		}
	}
	bin := filepath.Join(dist, "bin/linux-arm64/cn")
	raw, _ := os.ReadFile(bin)
	raw[len(raw)/2] ^= 1
	_ = os.WriteFile(bin, raw, 0o755)
	if _, err := sumsCheck(dist); err == nil {
		t.Error("sha256sum -c passed with a flipped byte in a binary")
	}
}

func TestSignKeySources(t *testing.T) {
	t.Parallel()
	dist, _ := scripttest.UnsignedDist(t)
	out, err := scripttest.Run(t, []string{"RELEASE_KEY=", "RELEASE_CERT=", "ROOTS=", "DIST=" + dist}, "dev/release/create/sign.sh")
	if err == nil {
		t.Fatalf("sign.sh signed with no keys\n%s", out)
	}
	if !strings.Contains(out, "RELEASE_KEY") || !strings.Contains(out, "RELEASE_CERT") {
		t.Errorf("message does not name RELEASE_KEY and RELEASE_CERT:\n%s", out)
	}
	// The development release key is certified by the development root,
	// which a released cn does not trust: by default its signature fails.
	dev := []string{"RELEASE_KEY=" + scripttest.Path(t, "dev/release/keys/testkeys/release.key"), "RELEASE_CERT=" + scripttest.Path(t, "dev/release/keys/testkeys/release.cert.json"), "ROOTS=", "DIST=" + dist}
	if out, err := scripttest.Run(t, dev, "dev/release/create/sign.sh"); err == nil {
		t.Fatalf("sign.sh accepted the development key against cn/packaging/trust/roots\n%s", out)
	} else if !strings.Contains(out, "not signed by a trusted root") || strings.Contains(out, "no trusted roots in") {
		t.Errorf("sign.sh did not refuse the development key on cn/packaging/trust/roots' own roots:\n%s", out)
	}
	dist, _ = scripttest.UnsignedDist(t)
	if out, err := scripttest.Run(t, append(scripttest.DevKeyEnv(t), "DIST="+dist), "dev/release/create/sign.sh"); err != nil {
		t.Fatalf("sign.sh with the development key against cn/packaging/trust/devroots: %v\n%s", err, out)
	}
}

func TestSignRefusesAnExpiringCertificate(t *testing.T) {
	t.Parallel()
	dist, _ := scripttest.UnsignedDist(t)
	rootRaw, _ := os.ReadFile(scripttest.Path(t, "dev/release/keys/testkeys/root.key"))
	root, err := sign.ParsePrivateKey(string(rootRaw))
	if err != nil {
		t.Fatal(err)
	}
	pubRaw, _ := os.ReadFile(scripttest.Path(t, "dev/release/keys/testkeys/release.pub"))
	pub, err := sign.ParsePublicKey(string(pubRaw))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert, err := sign.Issue(root, pub, sign.UseManifest, now.Add(-time.Hour), now.Add(10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(t.TempDir(), "release.cert.json")
	raw, _ := json.Marshal(cert)
	_ = os.WriteFile(certPath, raw, 0o644)
	out, err := scripttest.Run(t, []string{"RELEASE_KEY=" + scripttest.Path(t, "dev/release/keys/testkeys/release.key"), "RELEASE_CERT=" + certPath, "ROOTS=" + scripttest.Path(t, "cn/packaging/trust/devroots"), "DIST=" + dist}, "dev/release/create/sign.sh")
	if err == nil {
		t.Fatalf("sign.sh signed with a certificate expiring in 10 days\n%s", out)
	}
	if !strings.Contains(out, "Rotating the release key") {
		t.Errorf("message does not name the rotation step:\n%s", out)
	}
}
