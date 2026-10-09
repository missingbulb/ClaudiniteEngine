package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/usage"
)

// usageMounts are where a declared pack of each kind is mounted, as the
// Node fold's corpus reads them.
var usageMounts = map[string]string{
	"canon": ".claudinite/shared/packs",
	"local": ".claudinite/local/packs",
	"temp":  ".claudinite/temp/packs",
}

// usageWorld is one run of the usage fold: the member checkout, the
// instant it reads as now, and the API server that answers its REST reads.
type usageWorld struct {
	Root, Repo, Now string
	API             string
	Pack, Task      string
	Automerge       *string
	Packs           []struct{ ID, Kind string }
	PackConfig      map[string]any
	Target          struct {
		Branch string
		PR     *int
	}
}

// apiReader answers the folds' REST reads from the world's API server: a
// status other than 200 is no answer, and a server that cannot be reached
// an error. Each read is one request on a fresh connection: net/http
// sends a GET again when a kept-alive connection drops before answering,
// which fetch never does, and the harness counts requests.
type apiReader struct{ base string }

var oneShot = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

func (a apiReader) get(path string) (*string, error) {
	res, err := oneShot.Get(a.base + path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, nil
	}
	s := string(body)
	return &s, nil
}

func (a apiReader) JSON(path string) (any, error) {
	b, err := a.get(path)
	if b == nil || err != nil {
		return nil, err
	}
	v, err := usage.ParseJSON(*b)
	if err != nil {
		return nil, nil
	}
	return v, nil
}

func (a apiReader) Text(path string) (*string, error) { return a.get(path) }

// usageDecide runs the whole usage fold over a world's checkout and
// delivers on its target, as `cn usage fold` does with a job's client:
// remote git through the engine's runner, local git through the fold's.
func usageDecide(core string, raw []byte) (any, error) {
	if core != "fold" {
		return nil, fmt.Errorf("unknown usage core %q: one of [fold]", core)
	}
	var w usageWorld
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, err
	}
	if w.Root == "" || w.API == "" || w.Now == "" {
		return nil, fmt.Errorf("a usage world names its root, api and now")
	}
	git := gitcmd.Repo{Dir: w.Root}
	history := usage.History{
		Local: func(args ...string) (string, error) { return usage.RunLocal(w.Root, nil, "", args...) },
		Remote: func(args ...string) (string, error) {
			ran, err := git.Run(args...)
			if err != nil {
				return "", err
			}
			if ran.Code != 0 {
				return "", fmt.Errorf("git %s exited %d: %s", args[0], ran.Code, strings.TrimSpace(ran.Stderr))
			}
			return ran.Stdout, nil
		},
	}
	logs := []string{}
	log := func(s string) { logs = append(logs, s) }
	opened := []map[string]string{}
	answer := func(err error) map[string]any {
		out := map[string]any{"opened": opened, "error": nil, "logs": logs}
		if err != nil {
			out["error"] = err.Error()
		}
		return out
	}
	baseSha, err := usage.BaseTip(history, "main")
	if err != nil {
		return answer(err), nil
	}
	var dirs []string
	for _, p := range w.Packs {
		mount, ok := usageMounts[p.Kind]
		if !ok {
			mount = usageMounts["canon"]
		}
		dirs = append(dirs, filepath.Join(w.Root, mount, p.ID))
	}
	fold := usage.Fold{Root: w.Root, Repo: w.Repo, Base: "main", BaseSha: baseSha, Now: w.Now,
		Reader: apiReader{w.API}, History: history, Mounted: usage.MountedSkills(dirs),
		MinuteRate: usage.MinuteRateFrom(w.PackConfig), Log: log}
	trailers := ""
	if w.Pack != "" && w.Task != "" {
		trailers = "Claudinite-Task: " + w.Pack + "/" + w.Task
	}
	if w.Automerge != nil && *w.Automerge != "" {
		if trailers != "" {
			trailers += "\n"
		}
		trailers += "Claudinite-Automerge-Policy: " + *w.Automerge
	}
	pr := 0
	if w.Target.PR != nil {
		pr = *w.Target.PR
	}
	delivery := usage.Delivery{Root: w.Root, Base: "main", Branch: w.Target.Branch, PR: pr, History: history, Trailers: trailers,
		OpenPr: func(title, body, head, base string) (int, error) {
			opened = append(opened, map[string]string{"title": title, "body": body, "head": head, "base": base})
			return 41, nil
		}}
	_, err = usage.DeliverFolds(fold.Halves(), delivery.Deliver, log)
	return answer(err), nil
}
