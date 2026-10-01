package version

import "testing"

// The table ClaudinitePacks' tools/release/index.test.mjs pins for
// compareVersions, plus the cases this reader adds: the two readers are one
// order.
func TestComparePack(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// ClaudinitePacks' compareVersions table.
		{"60928.1", "60928.2", -1},
		{"60928.2", "60930.1", -1},
		{"60820.10", "60820.1", 1},
		{"60101.1", "60101.1", 0},
		// This chunk's.
		{"60928.10", "60928.9", 1},
		{"60928.1", "60928.1.0", -1},
		{"1.0", "1.1", -1},
		{"2", "1.9.9", 1},
		{"18446744073709551616", "18446744073709551615", 1},
	}
	for _, c := range cases {
		got, err := ComparePack(c.a, c.b)
		if err != nil {
			t.Fatalf("ComparePack(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("ComparePack(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if back, _ := ComparePack(c.b, c.a); back != -c.want {
			t.Errorf("ComparePack(%q, %q) = %d, want %d", c.b, c.a, back, -c.want)
		}
	}
}

func TestComparePackRefusesNonNumeric(t *testing.T) {
	for _, s := range []string{"", "1.a", "v1", "1..2", ".1", "1.", "1.-2", "1.0 ", "1e3"} {
		if _, err := ComparePack(s, "1.0"); err == nil {
			t.Errorf("ComparePack(%q) accepted", s)
		}
		if _, err := ComparePack("1.0", s); err == nil {
			t.Errorf("ComparePack(_, %q) accepted", s)
		}
	}
}
