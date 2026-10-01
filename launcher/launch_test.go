package launcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const testVersion = "1.1.0"

var (
	tools    string // built cn and regstub
	host     = version.Platform()
	hostBin  = releasefiles.BinaryName(host)
	repoRoot string
)

func TestMain(m *testing.M) {
	code, err := setup(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func setup(m *testing.M) (int, error) {
	var err error
	if repoRoot, err = filepath.Abs(".."); err != nil {
		return 0, err
	}
	if tools, err = os.MkdirTemp("", "launch-test-tools-"); err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(tools) }()
	pkg := "github.com/missingbulb/ClaudiniteEngine/shared/version"
	for _, b := range []struct{ out, pkg, ldflags string }{
		{filepath.Join(tools, hostBin), "./cmd/cn", "-X " + pkg + ".version=" + testVersion},
		{filepath.Join(tools, "regstub"), "./release/regstub", ""},
	} {
		cmd := exec.Command("go", "build", "-ldflags", b.ldflags, "-o", b.out, b.pkg)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			return 0, fmt.Errorf("build %s: %v\n%s", b.pkg, err, out)
		}
	}
	return m.Run(), nil
}

// release is a registry's worth of tarballs plus the pin a member would carry.
type release struct {
	dist     string
	manifest []byte
	pin      string
}

type releaseOpts struct {
	pkg            string // default @claudinite/cli
	omitHost       bool
	wrongHostBytes bool // serve a binary that does not match the manifest
	padBinary      int  // extra bytes appended to the served binary (manifest updated)
}

func makeRelease(t *testing.T, o releaseOpts) release {
	t.Helper()
	if o.pkg == "" {
		o.pkg = "@claudinite/cli"
	}
	name := strings.TrimPrefix(o.pkg, "@claudinite/")
	dist := t.TempDir()
	tarballs := filepath.Join(dist, "tarballs")
	if err := os.MkdirAll(tarballs, 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := os.ReadFile(filepath.Join(tools, hostBin))
	if err != nil {
		t.Fatal(err)
	}
	if o.padBinary > 0 {
		real = append(real, make([]byte, o.padBinary)...)
	}
	bins := map[string]releasefiles.Binary{}
	for _, p := range version.Platforms {
		data := []byte("not a real binary for " + p)
		if p == host {
			if o.omitHost {
				continue
			}
			data = real
		}
		sum := sha256.Sum256(data)
		bins[p] = releasefiles.Binary{File: releasefiles.BinaryName(p), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
		served := data
		if p == host && o.wrongHostBytes {
			served = append(append([]byte{}, data...), 'X')
		}
		err := releasefiles.WriteTarball(filepath.Join(tarballs, fmt.Sprintf("%s-%s-%s.tgz", name, p, testVersion)), []releasefiles.TarFile{
			{Name: "package.json", Mode: 0o644, Data: []byte("{}\n")},
			{Name: "bin/" + releasefiles.BinaryName(p), Mode: 0o755, Data: served},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	manifest := releasefiles.Format(releasefiles.Manifest{V: 1, Version: testVersion, BuiltAt: "2026-10-01T00:00:00Z", Commit: "test", GoVersion: runtime.Version(), Binaries: bins})
	err = releasefiles.WriteTarball(filepath.Join(tarballs, fmt.Sprintf("%s-%s.tgz", name, testVersion)), []releasefiles.TarFile{
		{Name: "package.json", Mode: 0o644, Data: []byte("{}\n")},
		{Name: "manifest.json", Mode: 0o644, Data: manifest},
	})
	if err != nil {
		t.Fatal(err)
	}
	return release{dist: dist, manifest: manifest, pin: releasefiles.Integrity(manifest)}
}

type stub struct {
	url, ca, log string
}

func startStub(t *testing.T, dist string, extra ...string) stub {
	t.Helper()
	dir := t.TempDir()
	s := stub{ca: filepath.Join(dir, "ca.pem"), log: filepath.Join(dir, "requests.log")}
	ready := filepath.Join(dir, "ready")
	args := append([]string{"--dist", dist, "--ready", ready, "--ca-out", s.ca, "--log", s.log}, extra...)
	cmd := exec.Command(filepath.Join(tools, "regstub"), args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(ready)
		if err == nil {
			s.url = strings.TrimSpace(string(raw))
			return s
		}
		if time.Now().After(deadline) {
			t.Fatal("regstub did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s stub) requests(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(s.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(strings.ReplaceAll(string(raw), "GET ", ""))
}

func (s stub) reset(t *testing.T) {
	t.Helper()
	_ = os.Remove(s.log)
}

// member is a fixture repo with the real launcher and a temp HOME and cache.
type member struct {
	dir, home, cache string
	registry, ca     string
	env              []string
}

func newMember(t *testing.T, s stub) *member {
	t.Helper()
	m := &member{dir: t.TempDir(), home: t.TempDir(), cache: t.TempDir(), registry: s.url, ca: s.ca}
	if err := os.MkdirAll(filepath.Join(m.dir, ".claudinite"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(repoRoot, "launcher", "launch"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.dir, ".claudinite", "launch"), src, 0o755); err != nil {
		t.Fatal(err)
	}
	return m
}

func (m *member) settings(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.dir, ".claudinite", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func yaml(ver, pin string) string {
	return fmt.Sprintf("# member settings\nplan: public\nengine:\n  version: %q\n  manifest: %q\npacks:\n  - basics\n", ver, pin)
}

func (m *member) run(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{filepath.Join(m.dir, ".claudinite", "launch")}, args...)...)
	cmd.Dir = m.dir
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + m.home,
		"XDG_CACHE_HOME=" + m.cache,
		"CLAUDINITE_REGISTRY=" + m.registry,
		"CURL_CA_BUNDLE=" + m.ca,
		"NO_PROXY=127.0.0.1,localhost",
	}, m.env...)
	cmd.Stdin = strings.NewReader(stdin)
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

func (m *member) vdir() string { return filepath.Join(m.cache, "claudinite", testVersion) }

func (m *member) cachedNothing(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(m.vdir())
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("cache for %s holds %v, want nothing", testVersion, names)
	}
}

const sessionStartStdin = `{"session_id":"s1","cwd":"/repo","hook_event_name":"SessionStart","source":"startup"}`

func TestLauncher(t *testing.T) {
	rel := makeRelease(t, releaseOpts{})

	t.Run("01 happy path", func(t *testing.T) {
		s := startStub(t, rel.dist)
		m := newMember(t, s)
		m.settings(t, "settings.yaml", yaml(testVersion, rel.pin))
		out, errOut, code := m.run(t, sessionStartStdin, "hook", "session-start")
		if code != 0 || !strings.Contains(out, "Hello from cn") {
			t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errOut)
		}
		bin := filepath.Join(m.vdir(), hostBin)
		st, err := os.Stat(bin)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o555 {
			t.Errorf("cached binary mode %v, want 0555", st.Mode().Perm())
		}
		vst, _ := os.Stat(m.vdir())
		if vst.Mode().Perm() != 0o700 {
			t.Errorf("version dir mode %v, want 0700", vst.Mode().Perm())
		}
		link := filepath.Join(m.dir, ".claudinite", "bin", hostBin)
		if target, err := os.Readlink(link); err != nil || target != bin {
			t.Errorf(".claudinite/bin/cn -> %q (%v), want %s", target, err, bin)
		}
		want := []string{
			"/@claudinite/cli/-/cli-" + testVersion + ".tgz",
			"/@claudinite/cli-" + host + "/-/cli-" + host + "-" + testVersion + ".tgz",
		}
		if got := s.requests(t); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("requests %v, want %v", got, want)
		}
		left, _ := filepath.Glob(filepath.Join(m.vdir(), ".*"))
		if len(left) != 0 {
			t.Errorf("temporary files left in the cache: %v", left)
		}
	})

	t.Run("02 second run downloads nothing and re-hashes", func(t *testing.T) {
		s := startStub(t, rel.dist)
		m := newMember(t, s)
		m.settings(t, "settings.yaml", yaml(testVersion, rel.pin))
		if _, e, code := m.run(t, sessionStartStdin, "hook", "session-start"); code != 0 {
			t.Fatal(e)
		}
		s.reset(t)
		out, _, code := m.run(t, sessionStartStdin, "hook", "session-start")
		if code != 0 || !strings.Contains(out, "Hello from cn") {
			t.Fatalf("second run: exit %d %s", code, out)
		}
		if got := s.requests(t); len(got) != 0 {
			t.Fatalf("second run made requests: %v", got)
		}
		bin := filepath.Join(m.vdir(), hostBin)
		raw, _ := os.ReadFile(bin)
		raw[len(raw)/2] ^= 0xff
		_ = os.Chmod(bin, 0o755)
		_ = os.WriteFile(bin, raw, 0o755)
		_ = os.Chmod(bin, 0o555)
		out, _, code = m.run(t, sessionStartStdin, "hook", "session-start")
		if code != 0 || !strings.Contains(out, "Hello from cn") {
			t.Fatalf("after mutation: exit %d %s", code, out)
		}
		if got := s.requests(t); len(got) != 2 {
			t.Fatalf("mutated binary was not re-downloaded: %v", got)
		}
		fixed, _ := os.ReadFile(bin)
		orig, _ := os.ReadFile(filepath.Join(tools, hostBin))
		if !bytes.Equal(fixed, orig) {
			t.Fatal("cache still holds the mutated binary")
		}
	})

	t.Run("03 settings formats", func(t *testing.T) {
		s := startStub(t, rel.dist)
		formats := map[string]string{
			"settings.json": fmt.Sprintf("{\n  \"plan\": \"public\",\n  \"engine\": {\n    \"version\": %q,\n    \"manifest\": %q\n  },\n  \"packs\": [\"basics\"]\n}\n", testVersion, rel.pin),
			"settings.toml": fmt.Sprintf("plan = \"public\"\n\n[engine]\nversion = %q\nmanifest = %q\n\n[packs]\nbasics = true\n", testVersion, rel.pin),
		}
		for name, body := range formats {
			m := newMember(t, s)
			m.settings(t, name, body)
			if out, e, code := m.run(t, sessionStartStdin, "hook", "session-start"); code != 0 || !strings.Contains(out, "Hello from cn") {
				t.Errorf("%s: exit %d\n%s\n%s", name, code, out, e)
			}
		}
		tabs := newMember(t, s)
		tabs.settings(t, "settings.toml", fmt.Sprintf("[engine]\nversion\t=\t%q\nmanifest =\t%q\n", testVersion, rel.pin))
		if out, e, code := tabs.run(t, sessionStartStdin, "hook", "session-start"); code != 0 || !strings.Contains(out, "Hello from cn") {
			t.Errorf("tab-separated toml: exit %d\n%s\n%s", code, out, e)
		}
		oneLine := newMember(t, s)
		oneLine.settings(t, "settings.json", fmt.Sprintf(`{"engine":{"version":%q,"manifest":%q}}`, testVersion, rel.pin))
		if out, e, code := oneLine.run(t, sessionStartStdin, "hook", "session-start"); code != 0 || !strings.Contains(out, "Hello from cn") {
			t.Errorf("one-line json: exit %d\n%s\n%s", code, out, e)
		}

		none := newMember(t, s)
		_, e, code := none.run(t, "", "env", "install")
		if code == 0 || !strings.Contains(e, "exactly one of .claudinite/settings.yaml") {
			t.Errorf("no settings: exit %d, stderr %q", code, e)
		}
		two := newMember(t, s)
		two.settings(t, "settings.yaml", yaml(testVersion, rel.pin))
		two.settings(t, "settings.json", formats["settings.json"])
		_, e, code = two.run(t, "", "env", "install")
		if code == 0 || !strings.Contains(e, "exactly one of .claudinite/settings.yaml") {
			t.Errorf("two settings: exit %d, stderr %q", code, e)
		}
	})

	t.Run("04 strict patterns before any request", func(t *testing.T) {
		s := startStub(t, rel.dist)
		bad := map[string]string{
			"short version":       yaml("1.1", rel.pin),
			"version with suffix": yaml("1.1.0-rc1", rel.pin),
			"path in version":     yaml("../1.1.0", rel.pin),
			"shell in version":    yaml("1.1.0$(id)", rel.pin),
			"md5 manifest":        yaml(testVersion, "md5-abc"),
			"short manifest":      yaml(testVersion, rel.pin[:40]),
			"manifest no padding": yaml(testVersion, strings.TrimSuffix(rel.pin, "==")),
			"unquoted version":    strings.Replace(yaml(testVersion, rel.pin), `"`+testVersion+`"`, testVersion, 1),
			"duplicate version":   yaml(testVersion, rel.pin) + "engine:\n  version: \"1.2.0\"\n",
			"missing manifest":    "engine:\n  version: \"1.1.0\"\n",
			"bad package":         yaml(testVersion, rel.pin) + "",
		}
		bad["bad package"] = strings.Replace(yaml(testVersion, rel.pin), "packs:", "  package: \"@evil/cli\"\npacks:", 1)
		for name, body := range bad {
			s.reset(t)
			m := newMember(t, s)
			m.settings(t, "settings.yaml", body)
			_, e, code := m.run(t, "", "env", "install")
			if code == 0 {
				t.Errorf("%s: accepted", name)
			}
			if got := s.requests(t); len(got) != 0 {
				t.Errorf("%s: made requests %v (stderr %s)", name, got, e)
			}
		}
	})

	t.Run("05 hash mismatches refuse in every context", func(t *testing.T) {
		otherPin := releasefiles.Integrity([]byte("some other manifest"))
		wrongBin := makeRelease(t, releaseOpts{wrongHostBytes: true})
		cases := []struct {
			name string
			dist string
			pin  string
		}{
			{"manifest", rel.dist, otherPin},
			{"binary", wrongBin.dist, wrongBin.pin},
		}
		for _, c := range cases {
			s := startStub(t, c.dist)
			for _, ctx := range []struct {
				args     []string
				env      []string
				wantCode int
			}{
				{[]string{"hook", "session-start"}, nil, 0},
				{[]string{"hook", "stop"}, nil, 2},
				{[]string{"env", "install"}, nil, 1},
				{[]string{"hook", "session-start"}, []string{"GITHUB_ACTIONS=true"}, 1},
			} {
				m := newMember(t, s)
				m.env = ctx.env
				m.settings(t, "settings.yaml", yaml(testVersion, c.pin))
				out, e, code := m.run(t, sessionStartStdin, ctx.args...)
				label := c.name + " " + strings.Join(ctx.args, " ") + " " + strings.Join(ctx.env, " ")
				if code != ctx.wantCode {
					t.Errorf("%s: exit %d, want %d (stderr %s)", label, code, ctx.wantCode, e)
				}
				if strings.Contains(out, "Hello from cn") || strings.Contains(out, "{}") {
					t.Errorf("%s: the engine ran: %s", label, out)
				}
				if !strings.Contains(e, "does not match") {
					t.Errorf("%s: stderr %q does not name the mismatch", label, e)
				}
				if ctx.args[1] == "session-start" && ctx.env == nil && !strings.HasPrefix(out, "Claudinite refused to run its engine: ") {
					t.Errorf("%s: stdout %q is not a halt directive", label, out)
				}
				m.cachedNothing(t)
			}
		}
	})

	t.Run("06 unlisted platform", func(t *testing.T) {
		r := makeRelease(t, releaseOpts{omitHost: true})
		s := startStub(t, r.dist)
		m := newMember(t, s)
		m.settings(t, "settings.yaml", yaml(testVersion, r.pin))
		_, e, code := m.run(t, "", "env", "install")
		if code == 0 || !strings.Contains(e, host) {
			t.Fatalf("exit %d, stderr %q", code, e)
		}
		if got := s.requests(t); len(got) != 1 {
			t.Errorf("requests %v, want the manifest only", got)
		}
		m.cachedNothing(t)
	})

	t.Run("07 download impossible", func(t *testing.T) {
		unavailable := startStub(t, rel.dist, "--status", "503")
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		dead := "https://" + ln.Addr().String()
		_ = ln.Close()
		for _, reg := range []string{unavailable.url, dead} {
			m := newMember(t, unavailable)
			m.registry = reg
			m.settings(t, "settings.yaml", yaml(testVersion, rel.pin))
			url := reg + "/@claudinite/cli/-/cli-" + testVersion + ".tgz"

			out, _, code := m.run(t, sessionStartStdin, "hook", "session-start")
			want := "Claudinite could not fetch its engine from " + url + ": stop and ask the person before continuing."
			if code != 0 || strings.TrimSpace(out) != want {
				t.Errorf("%s session-start: exit %d, stdout %q", reg, code, out)
			}
			out, e, code := m.run(t, "{}", "hook", "stop")
			if code != 0 || out != "" || !strings.Contains(e, "SessionStart") {
				t.Errorf("%s stop on a fresh cache: exit %d, stdout %q, stderr %q", reg, code, out, e)
			}
			m.env = []string{"GITHUB_ACTIONS=true"}
			if _, _, code := m.run(t, "{}", "hook", "stop"); code == 0 {
				t.Errorf("%s: Actions exit 0", reg)
			}
			m.cachedNothing(t)
		}

		// With a verified cached binary, a guard runs it whatever the network.
		good := startStub(t, rel.dist)
		m := newMember(t, good)
		m.settings(t, "settings.yaml", yaml(testVersion, rel.pin))
		if _, e, code := m.run(t, "", "env", "install"); code != 0 {
			t.Fatal(e)
		}
		m.registry = dead
		out, e, code := m.run(t, "{}", "hook", "stop")
		if code != 0 || strings.TrimSpace(out) != "{}" || !strings.Contains(e, "[cn] hooks stop ok") {
			t.Errorf("stop with a cached engine: exit %d, stdout %q, stderr %q", code, out, e)
		}
	})

	t.Run("08 size cap", func(t *testing.T) {
		r := makeRelease(t, releaseOpts{padBinary: 4096})
		s := startStub(t, r.dist)
		m := newMember(t, s)
		raw, _ := os.ReadFile(filepath.Join(tools, hostBin))
		m.env = []string{fmt.Sprintf("CLAUDINITE_MAX_DOWNLOAD_BYTES=%d", len(raw)/2)}
		m.settings(t, "settings.yaml", yaml(testVersion, r.pin))
		_, e, code := m.run(t, "", "env", "install")
		if code == 0 || !strings.Contains(e, "larger than") {
			t.Fatalf("exit %d, stderr %q", code, e)
		}
		m.cachedNothing(t)
	})

	t.Run("09 cache directory must be private", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no POSIX modes")
		}
		s := startStub(t, rel.dist)
		m := newMember(t, s)
		m.settings(t, "settings.yaml", yaml(testVersion, rel.pin))
		open := filepath.Join(m.cache, "claudinite")
		if err := os.Mkdir(open, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(open, 0o755)
		_, e, code := m.run(t, "", "env", "install")
		if code == 0 || !strings.Contains(e, open) {
			t.Fatalf("exit %d, stderr %q", code, e)
		}
		if got := s.requests(t); len(got) != 0 {
			t.Errorf("requests %v", got)
		}
	})

	t.Run("10 env install fetches and stops", func(t *testing.T) {
		s := startStub(t, rel.dist)
		m := newMember(t, s)
		m.settings(t, "settings.yaml", yaml(testVersion, rel.pin))
		out, e, code := m.run(t, "", "env", "install")
		if code != 0 || out != "" {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, out, e)
		}
		if _, err := os.Stat(filepath.Join(m.vdir(), hostBin)); err != nil {
			t.Fatal("env install placed no binary")
		}
		if _, err := os.Lstat(filepath.Join(m.dir, ".claudinite", "bin")); err == nil {
			t.Error("env install linked .claudinite/bin, which is step 5")
		}
	})

	t.Run("11 shellcheck", func(t *testing.T) {
		if _, err := exec.LookPath("shellcheck"); err != nil {
			t.Skip("shellcheck not installed; CI runs it")
		}
		out, err := exec.Command("shellcheck", "-s", "sh", filepath.Join(repoRoot, "launcher", "launch")).CombinedOutput()
		if err != nil {
			t.Fatalf("shellcheck: %v\n%s", err, out)
		}
	})

	t.Run("12 release channel package", func(t *testing.T) {
		r := makeRelease(t, releaseOpts{pkg: "@claudinite/cli-rc"})
		s := startStub(t, r.dist)
		m := newMember(t, s)
		m.settings(t, "settings.yaml", strings.Replace(yaml(testVersion, r.pin), "packs:", "  package: \"@claudinite/cli-rc\"\npacks:", 1))
		if _, e, code := m.run(t, "", "env", "install"); code != 0 {
			t.Fatal(e)
		}
		want := []string{
			"/@claudinite/cli-rc/-/cli-rc-" + testVersion + ".tgz",
			"/@claudinite/cli-rc-" + host + "/-/cli-rc-" + host + "-" + testVersion + ".tgz",
		}
		if got := s.requests(t); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("requests %v, want %v", got, want)
		}
	})
}
