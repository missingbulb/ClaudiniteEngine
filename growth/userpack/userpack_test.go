package userpack

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(root, rel string) string {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "<absent>"
	}
	return string(b)
}

// member declares the pack with config (a YAML mapping body, "" for
// none).
func member(t *testing.T, config string) string {
	repo := t.TempDir()
	entry := "    - claude-code-web-users-support\n"
	if config != "" {
		entry = "    - id: claude-code-web-users-support\n      config:\n" + config
	}
	put(t, repo, ".claudinite/settings.yaml", "packs:\n  declared:\n"+entry)
	return repo
}

// github answers GET /user with login for the token "good".
func github(t *testing.T, login string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"login": %q}`, login)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func env(vars map[string]string) Env {
	return Env{Getenv: func(k string) string { return vars[k] }, Client: http.DefaultClient}
}

func TestResolveStoreAndIdentityAreUserPackAddresssRules(t *testing.T) {
	for _, c := range []struct {
		config map[string]any
		want   string
	}{
		{map[string]any{"repo": "acme/store"}, "acme/store preferences"},
		{map[string]any{"repo": "acme/store", "path": "./people/"}, "acme/store people"},
		{map[string]any{"repo": "acme/store", "path": "/abs"}, "nil"},
		{map[string]any{"repo": "acme/store", "path": "a/../b"}, "nil"},
		{map[string]any{"repo": "acme"}, "nil"},
		{nil, "nil"},
	} {
		got := "nil"
		if s := ResolveStore(c.config); s != nil {
			got = s.Repo + " " + s.Path
		}
		if got != c.want {
			t.Errorf("ResolveStore(%v) = %s, want %s", c.config, got, c.want)
		}
	}
	for name, want := range map[string]bool{"ariel": true, "a-b": true, "a1": true, "-a": false, "a-": false, "a--b": false, "Ariel": false, "../x": false, strings.Repeat("a", 39): true, strings.Repeat("a", 40): false} {
		if UsableIdentity(name) != want {
			t.Errorf("UsableIdentity(%q) = %v", name, !want)
		}
	}
}

func TestPrepareTouchesNothingWhereThePackIsNotDeclared(t *testing.T) {
	repo := t.TempDir()
	put(t, repo, ".claudinite/settings.yaml", "packs:\n  declared:\n    - basics\n")
	if _, ok := Prepare(repo, env(nil)); ok {
		t.Fatal("ran where the pack is not declared")
	}
	if _, err := os.Stat(filepath.Join(repo, ".claudinite/temp")); !os.IsNotExist(err) {
		t.Error("wrote the session root anyway")
	}
}

func TestPrepareLeavesThePlaceholderAndSaysWhy(t *testing.T) {
	url := github(t, "ariel")
	for _, c := range []struct {
		name, config string
		vars         map[string]string
		line         string
	}{
		{"no store", "", nil, "declares no store for personal packs"},
		{"unattended", "        repo: acme/store\n", map[string]string{"CLAUDE_CODE_SESSION_ATTENDED": "0"}, "the session is unattended"},
		{"no token", "        repo: acme/store\n", nil, "no GitHub login was read for this session (neither GH_TOKEN nor GITHUB_TOKEN is set)"},
		{"refused", "        repo: acme/store\n", map[string]string{"GH_TOKEN": "bad", "CLAUDINITE_GITHUB_USER_URL": url}, "(GET /user answered no login (GH_TOKEN: HTTP 401))"},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo := member(t, c.config)
			r, ok := Prepare(repo, env(c.vars))
			if !ok || !strings.Contains(r.Line(), c.line) || !strings.HasSuffix(r.Line(), "proceeding with default interaction behavior.") {
				t.Errorf("line %q, want it to say %q", r.Line(), c.line)
			}
			if read(repo, PackDir+"/RULES.md") != Placeholder {
				t.Error("no placeholder")
			}
			if !strings.Contains(read(repo, ".claudinite/temp/.gitignore"), "*\n") {
				t.Error("the session root does not ignore itself")
			}
		})
	}
}

// The working tree is read first where this repo is the store, so the
// owner's edit in progress is what the session gets, under a synthesized
// manifest when the person's directory carries none, within the budget.
func TestPrepareCopiesFromTheWorkingTreeFirst(t *testing.T) {
	repo := member(t, "        repo: acme/store\n        path: people\n")
	put(t, repo, "people/ariel/RULES.md", "- my rule\n")
	put(t, repo, "people/ariel/skills/mine/SKILL.md", "mine\n")
	put(t, repo, "people/ariel/notes.txt", "not copied\n")
	put(t, repo, PackDir+"/stale.md", "an earlier person's\n")
	r, ok := Prepare(repo, env(map[string]string{"GITHUB_TOKEN": "good", "CLAUDINITE_GITHUB_USER_URL": github(t, "Ariel")}))
	if !ok || r.Line() != "[cn] personal pack: copied people/ariel/ from acme/store for GitHub user Ariel." {
		t.Fatalf("line %q", r.Line())
	}
	if read(repo, PackDir+"/RULES.md") != "- my rule\n" || read(repo, PackDir+"/skills/mine/SKILL.md") != "mine\n" {
		t.Error("the pack did not land")
	}
	if read(repo, PackDir+"/notes.txt") != "<absent>" || read(repo, PackDir+"/stale.md") != "<absent>" {
		t.Error("copied a file outside the copyable set, or kept an earlier pack")
	}
	if !strings.Contains(read(repo, PackDir+"/pack.json"), "how this person wants to be worked with") {
		t.Error("no synthesized manifest")
	}
	if !strings.Contains(read(repo, LoginRecord), `"login":"Ariel","via":"GITHUB_TOKEN"`) {
		t.Errorf("login record %q", read(repo, LoginRecord))
	}
	big := strings.Repeat("x", MaxBytes)
	put(t, repo, "people/ariel/huge.md", big)
	if r, _ := Prepare(repo, env(map[string]string{"GITHUB_TOKEN": "good", "CLAUDINITE_GITHUB_USER_URL": github(t, "ariel")})); r.Copied || read(repo, PackDir+"/RULES.md") != Placeholder || read(repo, PackDir+"/huge.md") != "<absent>" {
		t.Errorf("a pack past the byte budget leaves only the placeholder: %q", r.Line())
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Elsewhere the store is cloned sparse; a store with no directory for the
// person, or one that cannot be cloned, leaves the placeholder.
func TestPrepareClonesTheStore(t *testing.T) {
	store := t.TempDir()
	gitIn(t, store, "init", "-q")
	put(t, store, "preferences/ariel/RULES.md", "- from the store\n")
	put(t, store, "preferences/ariel/pack.json", `{"ruleRoutingGuidance": {"belongs": "me", "excludes": "x"}}`)
	put(t, store, "preferences/other/RULES.md", "- someone else\n")
	gitIn(t, store, "add", "-A")
	gitIn(t, store, "commit", "-q", "-m", "store")
	login := github(t, "ariel")
	vars := map[string]string{"GH_TOKEN": "good", "CLAUDINITE_GITHUB_USER_URL": login, "CLAUDINITE_USER_PACKS_CLONE_URL": "file://" + store}
	repo := member(t, "        repo: acme/store\n")
	r, _ := Prepare(repo, env(vars))
	if !r.Copied || read(repo, PackDir+"/RULES.md") != "- from the store\n" || !strings.Contains(read(repo, PackDir+"/pack.json"), `"belongs": "me"`) {
		t.Fatalf("clone copy: %q, err %v", r.Line(), r.Err)
	}
	if read(repo, PackDir+"/../other/RULES.md") != "<absent>" {
		t.Error("copied another person's pack")
	}
	vars["CLAUDINITE_GITHUB_USER_URL"] = github(t, "nobody")
	if r, _ := Prepare(repo, env(vars)); r.Copied || r.Line() != "[cn] personal pack: acme/store holds no pack at preferences/nobody/ for GitHub user nobody, or it could not be read - proceeding with default interaction behavior." {
		t.Errorf("no directory: %q", r.Line())
	}
	if read(repo, PackDir+"/RULES.md") != "- from the store\n" || read(repo, PackDir+"/pack.json") == "<absent>" {
		t.Error("a miss replaced the pack an earlier session copied, which the Node step keeps")
	}
	vars["CLAUDINITE_USER_PACKS_CLONE_URL"] = "file://" + filepath.Join(t.TempDir(), "missing")
	if r, _ := Prepare(repo, env(vars)); r.Copied || r.Err == nil || !strings.Contains(r.Line(), "(git clone: ") || read(repo, PackDir+"/RULES.md") != "- from the store\n" {
		t.Errorf("an unreachable store: %q", r.Line())
	}
}
