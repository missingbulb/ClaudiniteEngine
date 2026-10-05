package release

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// fakeNPM serves a tarball only to a request carrying a query until the
// first plain request, which it answers with a miss it then keeps, as the
// registry's CDN does; it records every request it saw.
type fakeNPM struct {
	mu      sync.Mutex
	ready   bool
	pinned  bool
	request []string
}

func (f *fakeNPM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.request = append(f.request, r.URL.RequestURI())
	switch {
	case !f.ready:
		w.WriteHeader(http.StatusNotFound)
	case r.URL.RawQuery != "":
		w.WriteHeader(http.StatusOK)
	case f.pinned:
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusOK)
	}
	if r.URL.RawQuery == "" && !f.ready {
		f.pinned = true
	}
}

func npmWait(t *testing.T, registry, timeout string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "release/npm-wait.sh", "--registry", registry, "--package", "@claudinite/cli-rc",
		"--version", "1.61005.3", "--platform", "linux-x64", "--timeout", timeout)
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestNPMWaitLooksAtTheLauncherURLsOnlyOnceNPMServesThem(t *testing.T) {
	f := &fakeNPM{ready: true}
	srv := httptest.NewServer(f)
	defer srv.Close()
	out, err := npmWait(t, srv.URL, "20")
	if err != nil {
		t.Fatalf("npm-wait failed on a registry that serves the release: %v\n%s", err, out)
	}
	want := []string{
		"/@claudinite/cli-rc/-/cli-rc-1.61005.3.tgz?",
		"/@claudinite/cli-rc-linux-x64/-/cli-rc-linux-x64-1.61005.3.tgz?",
		"/@claudinite/cli-rc/-/cli-rc-1.61005.3.tgz",
		"/@claudinite/cli-rc-linux-x64/-/cli-rc-linux-x64-1.61005.3.tgz",
	}
	if len(f.request) != len(want) {
		t.Fatalf("requests %q, want one fresh and one launcher look at each tarball", f.request)
	}
	for i, w := range want {
		got := f.request[i]
		if strings.HasSuffix(w, "?") && !strings.HasPrefix(got, w) || !strings.HasSuffix(w, "?") && got != w {
			t.Errorf("request %d = %q, want %q", i, got, w)
		}
	}
	if !strings.Contains(out, "npm-wait: served after") {
		t.Errorf("no served line:\n%s", out)
	}
}

func TestNPMWaitNeverTouchesALauncherURLBeforeNPMHasTheRelease(t *testing.T) {
	f := &fakeNPM{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	out, err := npmWait(t, srv.URL, "6")
	if err == nil {
		t.Fatalf("npm-wait passed a registry that never serves the release:\n%s", out)
	}
	if !strings.Contains(out, "npm still does not serve @claudinite/cli-rc 1.61005.3 for linux-x64 after 6 seconds") {
		t.Errorf("the timeout does not name the release:\n%s", out)
	}
	if f.pinned {
		t.Errorf("a launcher URL was looked at before npm served the release: %q", f.request)
	}
}
