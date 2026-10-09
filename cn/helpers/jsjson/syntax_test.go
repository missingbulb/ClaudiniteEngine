package jsjson

import (
	"encoding/json"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// testdata/syntax-errors.json is [text, message] pairs, each message the
// one Node 22's JSON.parse threw for the text ("" for none).
func TestSyntaxErrorIsV8s(t *testing.T) {
	raw, err := os.ReadFile("testdata/syntax-errors.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases [][2]string
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if got := SyntaxError(c[0]); got != c[1] {
			t.Errorf("SyntaxError(%q)\n got %q\nwant %q", c[0], got, c[1])
		}
	}
}

// Against the Node on PATH when it is Node 22: random mutations of JSON
// documents, each message compared. Skipped elsewhere, since V8's wording
// moves between majors.
func TestSyntaxErrorAgainstNode(t *testing.T) {
	out, err := exec.Command("node", "--version").Output()
	if err != nil || !strings.HasPrefix(string(out), "v22.") {
		t.Skip("needs Node 22 on PATH")
	}
	seeds := []string{`{"widgets": [{"id": "a", "kind": "stat", "noun": "n"}], "repo": ["a"], "views": {"main": "a"}}`,
		"[1, -2.5e+3, true, false, null, \"é\\u00e9\\n\", {\"k\": []}]", "{\n  \"a\": {\"b\": [0, 10, \"x\"]}\r\n}"}
	alphabet := []string{"{", "}", "[", "]", ":", ",", "\"", "\\", "-", "0", "1", ".", "e", "+", "t", "n", "u", "x", " ", "\n", "\r", "\x01", "é", "😀"}
	r := rand.New(rand.NewSource(65))
	var texts []string
	for i := 0; i < 600; i++ {
		s := []rune(seeds[i%len(seeds)])
		for k := 0; k < 1+r.Intn(3); k++ {
			at := r.Intn(len(s) + 1)
			switch r.Intn(3) {
			case 0:
				s = append(s[:at], append([]rune(alphabet[r.Intn(len(alphabet))]), s[at:]...)...)
			case 1:
				if at < len(s) {
					s = append(s[:at], s[at+1:]...)
				}
			default:
				s = s[:at]
			}
		}
		texts = append(texts, string(s))
	}
	in, _ := json.Marshal(texts)
	cmd := exec.Command("node", "-e", `const t = JSON.parse(require('fs').readFileSync(0, 'utf8'));
process.stdout.write(JSON.stringify(t.map((s) => { try { JSON.parse(s); return ''; } catch (e) { return e.message; } })));`)
	cmd.Stdin = strings.NewReader(string(in))
	got, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(got, &want); err != nil {
		t.Fatal(err)
	}
	bad := 0
	for i, s := range texts {
		if m := SyntaxError(s); m != want[i] && bad < 10 {
			bad++
			t.Errorf("SyntaxError(%q)\n got %q\nwant %q", s, m, want[i])
		}
	}
}
