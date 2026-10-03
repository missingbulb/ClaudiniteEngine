package jsjson

import "testing"

// Each want is what node prints for (x).toFixed(d).
func TestToFixedMatchesNumberToFixed(t *testing.T) {
	for _, c := range []struct {
		x    float64
		d    int
		want string
	}{
		{10.25, 1, "10.3"}, {0.25, 1, "0.3"}, {1.005, 2, "1.00"}, {2.5, 0, "3"}, {-2.5, 0, "-3"},
		{10.04, 1, "10.0"}, {0, 1, "0.0"}, {123.456, 1, "123.5"}, {0.05, 1, "0.1"}, {-0.04, 1, "-0.0"},
	} {
		if got := ToFixed(c.x, c.d); got != c.want {
			t.Errorf("ToFixed(%v, %d) = %q, want %q", c.x, c.d, got, c.want)
		}
	}
}
