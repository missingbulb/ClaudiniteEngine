package packs

import (
	"bytes"
	"net/http"
	"testing"
)

func sourceNames(srcs []Source) []string {
	var out []string
	for _, s := range srcs {
		switch v := s.(type) {
		case CDN:
			out = append(out, "cdn "+v.Base)
		case *Branch:
			out = append(out, "branch "+v.Repo+" as "+v.Name())
		}
	}
	return out
}

func TestSourcesForNoneReadsTheShelf(t *testing.T) {
	t.Setenv("CLAUDINITE_PACKS_CDN", "")
	t.Setenv("CLAUDINITE_PACKS_REPO", "")
	srcs, closer := SourcesFor(nil, http.DefaultClient)
	defer closer()
	got := sourceNames(srcs)
	want := []string{"cdn " + DefaultCDN, "branch " + DefaultRepo + " as branch"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("%v, want %v", got, want)
	}
}

func TestSourcesForNamesEachInOrder(t *testing.T) {
	srcs, closer := SourcesFor([]string{"https://packs.example.com", "acme/packs"}, http.DefaultClient)
	defer closer()
	got := sourceNames(srcs)
	want := []string{"cdn https://packs.example.com", "branch https://github.com/acme/packs as acme/packs"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("%v, want %v", got, want)
	}
}

func TestARepoListingOneSourceReadsOnlyIt(t *testing.T) {
	srcs, closer := SourcesFor([]string{"acme/packs"}, http.DefaultClient)
	defer closer()
	if got := sourceNames(srcs); len(got) != 1 || got[0] != "branch https://github.com/acme/packs as acme/packs" {
		t.Fatalf("%v", got)
	}
}

func TestVerifiedKeepsTheBytesItVerified(t *testing.T) {
	ix := indexJSON(5, "1.0")
	sig := signed(t, ix)
	v, err := newReader(&bytes.Buffer{}, &fakeSource{name: "acme/packs", pairs: []pair{{ix, sig}}}).VerifiedIndex("hello")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v.Raw, ix) || !bytes.Equal(v.Sig, sig) || v.From != "acme/packs" {
		t.Fatalf("%q %q %s", v.Raw, v.Sig, v.From)
	}
}
