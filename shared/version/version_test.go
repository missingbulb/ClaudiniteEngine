package version

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	v, err := Parse("60928.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if v.Day != 60928 || v.Ordinal != 1 || v.Patch != 0 {
		t.Fatalf("got %+v", v)
	}
}

func TestParseRefusesMalformed(t *testing.T) {
	for _, s := range []string{"", "1.1", "1.1.0.0", "a.1.0", "1.-1.0", " 1.1.0", "01.1.0", "1.1.0\n"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}

func TestCompareIsNumeric(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"60928.10.0", "60928.9.0", 1},
		{"60928.9.0", "60928.10.0", -1},
		{"60928.1.0", "60928.1.0", 0},
		{"60929.1.0", "60928.99.0", 1},
		{"1.1.0", "60928.1.0", -1},
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
}

func TestDayNumber(t *testing.T) {
	if got := DayNumber(time.Date(2026, 9, 28, 23, 0, 0, 0, time.UTC)); got != 60928 {
		t.Fatalf("got %d", got)
	}
	if got := DayNumber(time.Date(2031, 1, 2, 0, 0, 0, 0, time.UTC)); got != 110102 {
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
