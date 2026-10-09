package interview

import (
	"reflect"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

func pack(id string, kind packset.Kind, qs ...string) packset.Pack {
	p := packset.Pack{ID: id, Kind: kind}
	for _, q := range qs {
		p.Manifest.Questions = append(p.Manifest.Questions, packset.Question{ID: q, Prompt: q + "?"})
	}
	return p
}

func TestState(t *testing.T) {
	set := packset.Set{
		Declared: settings.Packs{Entries: []settings.PackEntry{
			{ID: "asks"},
			{ID: "answered", Object: true, Answers: map[string]string{"a": "n/a", "gone": "x"}},
			{ID: "mine", Local: true},
		}},
		Packs: []packset.Pack{
			pack("asks", packset.Canon, "a", "b"),
			pack("answered", packset.Canon, "a"),
			pack("mine", packset.Local, "c"),
			pack("visitor", packset.Temp, "d"),
		},
	}
	pending, stale := State(set)
	var got []string
	for _, p := range pending {
		for _, q := range p.Questions {
			got = append(got, p.Pack.ID+"/"+q.ID)
		}
	}
	if want := []string{"asks/a", "asks/b", "mine/c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pending %v, want %v", got, want)
	}
	if Count(pending) != 3 {
		t.Errorf("count %d", Count(pending))
	}
	if len(stale) != 1 || stale[0].Pack.ID != "answered" || stale[0].Answer != "gone" {
		t.Errorf("stale %+v", stale)
	}
}
