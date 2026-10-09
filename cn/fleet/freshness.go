package fleet

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// The freshness states, by root cause.
const (
	StateNode        = "node"
	StateNoStamp     = "no-stamp"
	StateNoScheduler = "no-scheduler"
	StateBehind      = "behind"
	StateFresh       = "fresh"
)

// NodeDetail is a node member's freshness row.
const NodeDetail = "runs the Node engine; moves with phase 9"

// Freshness is a member's state and why.
type Freshness struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// Shelf is where a member's own update reads what it would move to: the
// engine's npm packument and each pack's verified index. found false is a
// pack the shelf does not offer, which is no gap.
type Shelf interface {
	Packument(pkg string) (*npmreg.Packument, error)
	Index(id string) (ix packindex.Index, found bool, err error)
}

// Memo reads each packument and index once per sweep.
func Memo(s Shelf) Shelf { return &memo{Shelf: s, p: map[string]memoP{}, ix: map[string]memoIx{}} }

type memoP struct {
	p   *npmreg.Packument
	err error
}

type memoIx struct {
	ix    packindex.Index
	found bool
	err   error
}

type memo struct {
	Shelf
	mu sync.Mutex
	p  map[string]memoP
	ix map[string]memoIx
}

func (m *memo) Packument(pkg string) (*npmreg.Packument, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.p[pkg]; ok {
		return v.p, v.err
	}
	p, err := m.Shelf.Packument(pkg)
	m.p[pkg] = memoP{p, err}
	return p, err
}

func (m *memo) Index(id string) (packindex.Index, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.ix[id]; ok {
		return v.ix, v.found, v.err
	}
	ix, found, err := m.Shelf.Index(id)
	m.ix[id] = memoIx{ix, found, err}
	return ix, found, err
}

// FreshIn is what Classify judges: the member's own facts and what its
// update would move each of them to, "" for nothing.
type FreshIn struct {
	Shape        Shape
	Format       settings.Format
	HasScheduler bool
	Pin          string
	Held         map[string]string
	EngineNext   string
	PackNext     map[string]string
}

// Measure reads, for a covered awake member, what its own update would
// decide: npmreg.Candidate over the release package's packument, and
// packindex.Select over each declared pack's index. Only the reads the
// classification needs are made.
func Measure(m Member, hasScheduler bool, s Shelf) (FreshIn, error) {
	in := FreshIn{Shape: m.Shape, Format: m.Format, HasScheduler: hasScheduler, Pin: m.Pin.Version, Held: m.Held, PackNext: map[string]string{}}
	if m.Shape != ShapeCn || (in.Pin == "" && len(in.Held) == 0) || !hasScheduler {
		return in, nil
	}
	if in.Pin != "" {
		p, err := s.Packument(settings.DefaultPackage)
		if err != nil {
			return in, fmt.Errorf("reading %s from npm: %v", settings.DefaultPackage, err)
		}
		in.EngineNext = npmreg.Candidate(in.Pin, m.Pin.Channel, p, npmreg.StatesFromPackument(p)).Version
	}
	channel := m.Packs.Channel
	if channel == "" {
		channel = settings.ChannelStable
	}
	for _, id := range sortedKeys(in.Held) {
		ix, found, err := s.Index(id)
		if err != nil {
			return in, fmt.Errorf("reading %s's pack index: %v", id, err)
		}
		if !found {
			continue
		}
		if c := packindex.Select(ix, packindex.Want{Channel: channel, Engine: in.Pin, Held: in.Held[id]}); c.Entry != nil {
			in.PackNext[id] = c.Entry.Version
		}
	}
	return in, nil
}

// Classify is a member's freshness by root cause: a node member is not
// compared; no stamp before no scheduler before behind.
func Classify(in FreshIn) Freshness {
	if in.Shape == ShapeNode {
		return Freshness{StateNode, NodeDetail}
	}
	if in.Pin == "" && len(in.Held) == 0 {
		return Freshness{StateNoStamp, fmt.Sprintf("%s carries no readable engine pin and no declared pack holds a manifest version — the repo declares packs but has never been vendored", settings.RelPath(in.Format))}
	}
	if !in.HasScheduler {
		return Freshness{StateNoScheduler, "no " + SchedulerPath + " — the repo has no cron, so nothing there will ever converge it"}
	}
	var gaps []string
	if in.EngineNext != "" {
		gaps = append(gaps, "engine "+in.Pin+" → "+in.EngineNext)
	}
	for _, id := range sortedKeys(in.PackNext) {
		gaps = append(gaps, id+" "+in.Held[id]+" → "+in.PackNext[id])
	}
	if len(gaps) > 0 {
		return Freshness{StateBehind, "behind the published versions by " + strings.Join(gaps, ", ")}
	}
	pin := in.Pin
	if pin == "" {
		pin = "—"
	}
	return Freshness{StateFresh, fmt.Sprintf("engine %s, %d declared pack(s) at the published versions", pin, len(in.Held))}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
