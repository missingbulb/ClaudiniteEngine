package run

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a checks binary: FAKE_CHECKS picks how it
// behaves on the pipe.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_CHECKS"); mode != "" {
		fake(mode)
		return
	}
	os.Exit(m.Run())
}

func fake(mode string) {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 32<<20)
	if !in.Scan() {
		os.Exit(3)
	}
	switch mode {
	case "badproto":
		fmt.Println(`{"proto":"claudinite-checks-v0"}`)
		return
	case "silent":
		time.Sleep(time.Hour)
	}
	fmt.Println(`{"proto":"claudinite-checks-v1"}`)
	for in.Scan() {
		req := in.Text()
		switch mode {
		case "malformed":
			fmt.Println(`{"findings": [`)
		case "oversized":
			fmt.Println(`{"findings":[{"sentence":"` + strings.Repeat("x", 17<<20) + `"}]}`)
		case "silent-run":
			time.Sleep(time.Hour)
		case "env":
			fmt.Printf("{\"findings\":[{\"check\":\"t/env\",\"class\":\"advisory\",\"path\":\".\",\"sentence\":%q}]}\n", os.Getenv("SECRET_TOKEN")+"|"+os.Getenv("NODE_OPTIONS"))
		default:
			if strings.Contains(req, `"op":"list"`) {
				fmt.Println(`{"checks":[{"check":"hello/hello-check","tags":["work","world"]},{"check":"hello/hello-judge","tags":["pre-tool-use"],"judge":true}]}`)
				continue
			}
			if strings.Contains(req, `"op":"judge"`) {
				if !strings.Contains(req, `"event":"pre-tool-use"`) || !strings.Contains(req, `"call":{"tool":"Bash","input":{"command":"x"}}`) || !strings.Contains(req, `"repo":"/repo"`) {
					fmt.Printf("{\"error\":%q}\n", "unexpected request "+req)
					continue
				}
				fmt.Println(`{"findings":[{"check":"hello/hello-judge","class":"finding","path":"(tool call)","sentence":"no"}]}`)
				continue
			}
			if !strings.Contains(req, `"repo":"/repo"`) || !strings.Contains(req, `"tags":["work"]`) || !strings.Contains(req, `"pack":"hello"`) {
				fmt.Printf("{\"error\":%q}\n", "unexpected request "+req)
				continue
			}
			fmt.Println(`{"findings":[{"check":"hello/hello-check","class":"finding","path":"HELLO_FINDING","sentence":"delete it"},{"check":"hello/x","class":"advisory","path":".","sentence":"fyi"}],"errors":["hello/y: panic: boom"]}`)
		}
	}
}

func runner(t *testing.T, mode string) Runner {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CHECKS", mode)
	return Runner{Binary: exe, Engine: "1.1.0", Silence: 2 * time.Second, extraEnv: []string{"FAKE_CHECKS=" + mode}}
}

func TestRunReturnsFindingsAndErrors(t *testing.T) {
	res, crumb := runner(t, "good").Run("stop", []string{"work"}, "hello", "/repo")
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Findings) != 2 || res.Findings[0].Check != "hello/hello-check" || res.Findings[0].Class != "finding" || res.Findings[1].Class != "advisory" {
		t.Errorf("%+v", res.Findings)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "panic") {
		t.Errorf("errors %v", res.Errors)
	}
	if !res.Blocking() {
		t.Error("a finding does not block")
	}
	if !strings.HasPrefix(crumb, "[cn] checks stop error ") {
		t.Errorf("a run with a check error has breadcrumb %q", crumb)
	}
}

func TestList(t *testing.T) {
	checks, err := runner(t, "good").List()
	if err != nil || len(checks) != 2 || checks[0].Check != "hello/hello-check" || strings.Join(checks[0].Tags, ",") != "work,world" || checks[0].Judge || !checks[1].Judge {
		t.Errorf("%+v %v", checks, err)
	}
}

func TestJudgeSendsTheCall(t *testing.T) {
	res := runner(t, "good").Judge("pre-tool-use", Call{Tool: "Bash", Input: []byte(`{"command":"x"}`)}, "/repo")
	if res.Err != nil || len(res.Findings) != 1 || res.Findings[0].Check != "hello/hello-judge" || res.Findings[0].Class != "finding" {
		t.Errorf("%+v", res)
	}
	if res := runner(t, "silent-run").Judge("pre-tool-use", Call{Tool: "Bash"}, "/repo"); res.Err == nil {
		t.Error("a silent judge answered")
	}
}

func TestRunKillsAMisbehavingChild(t *testing.T) {
	cases := map[string]string{
		"badproto":   "protocol",
		"malformed":  "malformed",
		"oversized":  "too long",
		"silent":     "silent",
		"silent-run": "silent",
	}
	for mode, want := range cases {
		start := time.Now()
		res, crumb := runner(t, mode).Run("world", []string{"world"}, "", "/repo")
		if res.Err == nil || !strings.Contains(res.Err.Error(), want) {
			t.Errorf("%s: %v", mode, res.Err)
		}
		if time.Since(start) > 10*time.Second {
			t.Errorf("%s: took %v", mode, time.Since(start))
		}
		outcome := "error"
		if strings.HasPrefix(mode, "silent") {
			outcome = "timeout"
		}
		if !strings.HasPrefix(crumb, "[cn] checks world "+outcome+" ") {
			t.Errorf("%s: breadcrumb %q", mode, crumb)
		}
	}
}

func TestRunScrubsTheEnvironment(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "s3cret")
	t.Setenv("NODE_OPTIONS", "--require evil")
	res, crumb := runner(t, "env").Run("tag", nil, "", "/repo")
	if res.Err != nil || len(res.Findings) != 1 || res.Findings[0].Sentence != "|" {
		t.Errorf("%+v %v", res.Findings, res.Err)
	}
	if res.Blocking() || !strings.HasPrefix(crumb, "[cn] checks tag ok ") {
		t.Errorf("an advisory blocks, or breadcrumb %q", crumb)
	}
}

func TestRunWithoutBinary(t *testing.T) {
	res, crumb := Runner{Binary: "/nonexistent/checks", Engine: "1"}.Run("stop", []string{"work"}, "", "/repo")
	if res.Err == nil || !strings.Contains(crumb, " error ") {
		t.Errorf("%v %q", res.Err, crumb)
	}
}
