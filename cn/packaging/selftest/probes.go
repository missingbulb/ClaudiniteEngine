package selftest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/rules/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/report"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/workflows"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// Names are the probes in report order.
var Names = []string{"binary", "cache", "roots", "mount", "packs", "hooks", "skills", "scheduler", "rules", "crashes"}

// Run answers every probe in Names order.
func Run(in Input) []Probe {
	out := []Probe{
		{"binary", OK, in.Platform},
		cache(in),
		roots(in),
	}
	member := memberProbes(in)
	for _, name := range []string{"mount", "packs", "hooks", "skills", "scheduler", "rules"} {
		out = append(out, member(name))
	}
	n := report.CountRecent(report.CrashDir(in.CacheRoot), in.Now, 7*24*time.Hour)
	return append(out, Probe{"crashes", OK, fmt.Sprintf("%d in 7 days", n)})
}

func cache(in Input) Probe {
	if err := writable(in.CacheRoot); err != nil {
		return Probe{"cache", Fail, fmt.Sprintf("%s not writable: %v", in.CacheRoot, err)}
	}
	return Probe{"cache", OK, in.CacheRoot + " writable"}
}

func roots(in Input) Probe {
	if in.RootsErr != nil {
		return Probe{"roots", Fail, "invalid: " + in.RootsErr.Error()}
	}
	return Probe{"roots", OK, strings.Join(in.RootIDs, " ")}
}

// memberProbes answers the probes that read in.Repo, each a skip when
// there is no member to read.
func memberProbes(in Input) func(string) Probe {
	why := "no --repo"
	if in.Repo != "" {
		why = ""
		if _, _, err := settings.Find(in.Repo); err != nil {
			why = "not a member: " + err.Error()
		}
	}
	return func(name string) Probe {
		if why != "" {
			return Probe{name, Skip, why}
		}
		switch name {
		case "mount":
			return mount(in)
		case "packs":
			return packs(in)
		case "hooks":
			return hooks(in)
		case "skills":
			return skills(in)
		case "scheduler":
			return scheduler(in)
		}
		return rules(in)
	}
}

func mount(in Input) Probe {
	d, err := packset.Declared(in.Repo)
	if err != nil {
		return Probe{"mount", Fail, err.Error()}
	}
	if len(d.Declared) == 0 {
		return Probe{"mount", Skip, "no canon packs declared"}
	}
	var missing []string
	for _, id := range d.Declared {
		if _, err := packset.ReadManifest(packset.Tree(in.Repo, id)); errors.Is(err, packset.ErrNoManifest) {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return Probe{"mount", Fail, packset.Dir + " lacks " + strings.Join(missing, ", ")}
	}
	return Probe{"mount", OK, fmt.Sprintf("%d declared packs under %s", len(d.Declared), packset.Dir)}
}

func packs(in Input) Probe {
	s, err := packset.Load(in.Repo, in.Version, false)
	if err != nil {
		return Probe{"packs", Fail, err.Error()}
	}
	if len(s.NotLoaded) > 0 {
		var why []string
		for _, n := range s.NotLoaded {
			why = append(why, n.Token+": "+n.Why)
		}
		return Probe{"packs", Fail, strings.Join(why, "; ")}
	}
	if len(s.Packs) == 0 {
		return Probe{"packs", Skip, "no packs declared"}
	}
	return Probe{"packs", OK, fmt.Sprintf("%d load on %s", len(s.Packs), in.Version)}
}

var hookCall = regexp.MustCompile(`(?:\.claudinite/bin/cn|\.claudinite/launch"?)\s+hook\s+(\S+)`)

func hooks(in Input) Probe {
	raw, err := os.ReadFile(filepath.Join(in.Repo, ".claude", "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Probe{"hooks", Skip, "no .claude/settings.json"}
	}
	if err != nil {
		return Probe{"hooks", Fail, err.Error()}
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Probe{"hooks", Fail, ".claude/settings.json: " + err.Error()}
	}
	answers := map[string]bool{}
	for _, e := range in.HookEvents {
		answers[e] = true
	}
	events := make([]string, 0, len(cfg.Hooks))
	for e := range cfg.Hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	var bad []string
	n := 0
	for _, e := range events {
		for _, g := range cfg.Hooks[e] {
			for _, c := range g.Hooks {
				m := hookCall.FindStringSubmatch(c.Command)
				if m == nil {
					continue
				}
				n++
				if !answers[m[1]] {
					bad = append(bad, fmt.Sprintf("%s → cn hook %s, an event this binary does not answer", e, m[1]))
				}
			}
		}
	}
	if len(bad) > 0 {
		return Probe{"hooks", Fail, strings.Join(bad, "; ")}
	}
	if n == 0 {
		return Probe{"hooks", Skip, "no hook runs cn"}
	}
	return Probe{"hooks", OK, fmt.Sprintf("%d wired, each an event this binary answers", n)}
}

func skills(in Input) Probe {
	s, err := packset.Load(in.Repo, in.Version, false)
	if err != nil {
		return Probe{"skills", Fail, err.Error()}
	}
	var bad []string
	n := 0
	for _, p := range s.Packs {
		for _, sk := range p.Skills {
			file := filepath.Join(p.Dir, "skills", sk, "SKILL.md")
			raw, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			n++
			for _, l := range checksdk.ExtractLinks(string(raw)) {
				target := l.Target
				if strings.HasPrefix(target, "/") {
					continue
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(file), filepath.FromSlash(target))); err != nil {
					bad = append(bad, fmt.Sprintf("%s/%s: link → %s missing", p.ID, sk, target))
				}
			}
		}
	}
	if len(bad) > 0 {
		return Probe{"skills", Fail, strings.Join(bad, "; ")}
	}
	if n == 0 {
		return Probe{"skills", Skip, "no skills mounted"}
	}
	return Probe{"skills", OK, fmt.Sprintf("%d skills, every relative link resolves", n)}
}

func scheduler(in Input) Probe {
	dir := filepath.Join(in.Repo, ".github", "workflows")
	raw, err := os.ReadFile(filepath.Join(dir, "claudinite-scheduler.yml"))
	if err != nil {
		return Probe{"scheduler", Skip, "no scheduler workflow"}
	}
	if _, err := os.Stat(filepath.Join(dir, "claudinite-executor.yml")); err != nil {
		return Probe{"scheduler", Fail, "scheduler without executor: its items are never picked up"}
	}
	if strings.Contains(string(raw), `cron: "`+workflows.CronPlaceholder+`"`) {
		return Probe{"scheduler", OK, "with executor, on the templates' placeholder cron"}
	}
	return Probe{"scheduler", OK, "with executor"}
}

func rules(in Input) Probe {
	st, _, err := rulesindex.Check(in.Repo, in.Version)
	switch {
	case err != nil:
		return Probe{"rules", Fail, err.Error()}
	case st == rulesindex.Stale:
		return Probe{"rules", Fail, rulesindex.File + " is stale: sessions read another set of rules than the declared packs'"}
	case st == rulesindex.Absent:
		return Probe{"rules", Skip, "no " + rulesindex.File}
	case st == rulesindex.Empty:
		return Probe{"rules", Skip, "no declared pack has rules"}
	}
	return Probe{"rules", OK, rulesindex.File + " current"}
}
