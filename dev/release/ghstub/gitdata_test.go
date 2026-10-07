package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The manager's git data API writes a branch the way a fleet's mirror
// does: blobs, a tree on the last one's, a commit, a ref created and then
// moved forward only.
func TestTheManagerTakesAMirrorCommitByCommit(t *testing.T) {
	c, _ := startFleet(t)
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		status, raw, err := c.Raw(method, "/repos/acme/manager"+path, body)
		if err != nil || status != want {
			t.Fatalf("%s %s: %d %s %v", method, path, status, raw, err)
		}
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		return v
	}
	sha := func(v map[string]any) string { s, _ := v["sha"].(string); return s }
	call("GET", "/git/ref/heads/vendored", nil, 404)
	write := func(parent, base, path, b64 string) (string, string) {
		blob := sha(call("POST", "/git/blobs", map[string]string{"content": b64, "encoding": "base64"}, 201))
		body := map[string]any{"tree": []map[string]string{{"path": path, "mode": "100644", "type": "blob", "sha": blob}}}
		parents := []string{}
		if parent != "" {
			body["base_tree"], parents = base, []string{parent}
		}
		tree := sha(call("POST", "/git/trees", body, 201))
		return sha(call("POST", "/git/commits", map[string]any{"message": "m", "tree": tree, "parents": parents}, 201)), tree
	}
	first, tree := write("", "", "hello/1.0.tar.gz", "YQ==")
	call("POST", "/git/refs", map[string]any{"ref": "refs/heads/vendored", "sha": first}, 201)
	second, _ := write(first, tree, "hello/index.json", "Yg==")
	call("PATCH", "/git/refs/heads/vendored", map[string]any{"sha": second, "force": false}, 200)
	call("PATCH", "/git/refs/heads/vendored", map[string]any{"sha": first, "force": false}, 422)

	head := call("GET", "/git/ref/heads/vendored", nil, 200)["object"].(map[string]any)["sha"]
	if head != second {
		t.Fatalf("the branch is at %v", head)
	}
	treeSHA := call("GET", "/git/commits/"+second, nil, 200)["tree"].(map[string]any)["sha"].(string)
	status, raw, _ := c.Raw("GET", "/repos/acme/manager/git/trees/"+treeSHA+"?recursive=1", nil)
	for _, want := range []string{`"hello/1.0.tar.gz"`, `"hello/index.json"`, `"blob"`} {
		if status != 200 || !strings.Contains(string(raw), want) {
			t.Errorf("tree lacks %s: %d %s", want, status, raw)
		}
	}
}
