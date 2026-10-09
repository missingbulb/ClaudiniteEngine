package jsjson

import (
	"os"
	"os/exec"
	"testing"
)

func TestStringifyMatchesJavaScript(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"b":1,"a":[1,2.50,{"z":null,"y":true}]}`, `{"b":1,"a":[1,2.5,{"z":null,"y":true}]}`},
		{`{ "output_mode" : "content", "pattern":"<a&b>" }`, `{"output_mode":"content","pattern":"<a&b>"}`},
		{`{"b":1,"2":"x","1":"y","a":0}`, `{"1":"y","2":"x","b":1,"a":0}`},
		{`{"a":1,"b":2,"a":3}`, `{"a":3,"b":2}`},
		{`"tab\there é \u0001 \"q\" \\"`, `"tab\there é \u0001 \"q\" \\"`},
		{`[1e21, 1e-7, 0.000001, -0, 123456789012345680000, 1E2]`, `[1e+21,1e-7,0.000001,0,123456789012345680000,100]`},
		{`{}`, `{}`},
		{`[]`, `[]`},
	}
	for _, c := range cases {
		v, err := Decode([]byte(c.in))
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got := Stringify(v); got != c.want {
			t.Errorf("Stringify(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestFieldAt(t *testing.T) {
	v, _ := Decode([]byte(`{"a":{"b":"x","n":0,"arr":[{"c":1}]},"s":"str"}`))
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"a.b", `"x"`, true},
		{"a.n", `0`, true},
		{"a.n.deeper", `0`, true},
		{"a.arr.0.c", `1`, true},
		{"a.arr.length", `1`, true},
		{"s.length", ``, false},
		{"missing.x", ``, false},
	}
	for _, c := range cases {
		got, ok := FieldAt(v, c.path)
		if ok != c.ok || (ok && Stringify(got) != c.want) {
			t.Errorf("FieldAt(%s) = %v %v, want %s %v", c.path, Stringify(got), ok, c.want, c.ok)
		}
	}
}

// The same inputs through node's JSON.stringify(JSON.parse(x)), when node
// is at hand.
func TestAgainstNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil || os.Getenv("CI_NO_NODE") != "" {
		t.Skip("no node")
	}
	ins := []string{
		`{"z":{"y":[1,{"k":"  line"}]},"10":1,"9":2,"x":-1.5e-9}`,
		`{"command":"git push origin --delete x","description":"d"}`,
		`[0.1,1e300,-3,"\u001f"]`,
	}
	for _, in := range ins {
		out, err := exec.Command("node", "-e", "process.stdout.write(JSON.stringify(JSON.parse(process.argv[1])))", in).Output()
		if err != nil {
			t.Fatal(err)
		}
		v, err := Decode([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if got := Stringify(v); got != string(out) {
			t.Errorf("%s: go %s, node %s", in, got, out)
		}
	}
}
