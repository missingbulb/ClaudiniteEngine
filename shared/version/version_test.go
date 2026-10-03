package version

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	v, err := Parse("1.60928.1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Major != 1 || v.Day != 60928 || v.Build != 1 {
		t.Fatalf("got %+v", v)
	}
	if v, err := Parse("0.0.0"); err != nil || v != (V{}) {
		t.Fatalf("a dev build's 0.0.0: %+v %v", v, err)
	}
}

func TestParseRefusesMalformed(t *testing.T) {
	for _, s := range []string{"", "1.1", "1.1.0.0", "a.1.0", "1.-1.0", " 1.60928.1", "01.60928.1", "1.60928.1\n",
		"1.60928.0", "1.60900.1", "1.61300.1", "1.60932.1", "1.0.1", "0.0.1"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}

// A version in the retired <day>.<n>.0 format is refused by name, never
// read as a major release of that size.
func TestParseRefusesTheRetiredFormat(t *testing.T) {
	for _, s := range []string{"61003.1.0", "60928.12.0", "61001.1.0"} {
		_, err := Parse(s)
		if err == nil || !strings.Contains(err.Error(), "retired <day>.<n>.0 format") {
			t.Errorf("Parse(%q) = %v, want the retired format named", s, err)
		}
	}
}

func TestCompareIsNumeric(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.60928.10", "1.60928.9", 1},
		{"1.60928.9", "1.60928.10", -1},
		{"1.60928.1", "1.60928.1", 0},
		{"1.60929.1", "1.60928.99", 1},
		{"2.60101.1", "1.61231.9", 1},
		{"10.60101.1", "9.60101.1", 1},
		{"0.0.0", "1.60928.1", -1},
	}
	for _, c := range cases {
		got, err := Compare(c.a, c.b)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := Compare("61003.1.0", "1.61003.1"); err == nil {
		t.Error("Compare read a retired-format version")
	}
}

func TestToday(t *testing.T) {
	if got := Today(time.Date(2026, 9, 28, 23, 0, 0, 0, time.UTC)); got != 60928 {
		t.Fatalf("got %d", got)
	}
	if got := Today(time.Date(2031, 1, 2, 0, 0, 0, 0, time.UTC)); got != 110102 {
		t.Fatalf("got %d", got)
	}
	// 2026-09-28 23:30 in UTC-5 is already the 29th in UTC.
	if got := Today(time.Date(2026, 9, 28, 23, 30, 0, 0, time.FixedZone("x", -5*3600))); got != 60929 {
		t.Fatalf("got %d", got)
	}
}

func TestPlatformName(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:   "linux-x64",
		{"linux", "arm64"}:   "linux-arm64",
		{"darwin", "amd64"}:  "darwin-x64",
		{"darwin", "arm64"}:  "darwin-arm64",
		{"windows", "amd64"}: "windows-x64",
		{"plan9", "386"}:     "",
	}
	for in, want := range cases {
		if got := PlatformName(in[0], in[1]); got != want {
			t.Errorf("PlatformName(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDevBuildDefaults(t *testing.T) {
	if Version() != "0.0.0" {
		t.Fatalf("an uninjected build must report 0.0.0, got %q", Version())
	}
}

func TestParseMinEngineVersion(t *testing.T) {
	if _, err := ParseMinEngineVersion("60928.1"); !errors.Is(err, ErrNodeEngine) {
		t.Errorf("60928.1: %v, want ErrNodeEngine", err)
	}
	cur, err := ParseMinEngineVersion("1.60928.1")
	if err != nil {
		t.Fatalf("1.60928.1: %v", err)
	}
	for pin, want := range map[string]bool{"1.60928.1": true, "1.60928.2": true, "1.60929.1": true, "2.60101.1": true,
		"1.60927.9": false, "0.0.0": false, "61003.1.0": false, "bad": false} {
		if got := cur.Satisfies(pin); got != want {
			t.Errorf("1.60928.1 satisfies %s = %v, want %v", pin, got, want)
		}
	}
	for _, bad := range []string{"60928", "1.60928.1.0", "", "60928.01", "v60928.1", "60928.1.x", "61001.1.0"} {
		if _, err := ParseMinEngineVersion(bad); err == nil || errors.Is(err, ErrNodeEngine) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
