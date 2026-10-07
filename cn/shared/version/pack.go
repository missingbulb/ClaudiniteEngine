package version

import (
	"fmt"
	"strings"
)

// ComparePack orders two pack versions, the order of ClaudinitePacks'
// compareVersions, which writes the index. A pack version is
// <major>.<day>.<n> as Parse reads it, the dev build's 0.0.0 aside; one in
// any other dot-separated form, such as the two-part 61002.3 the shelf first
// published, sorts below every version in that form. Within either form,
// dot-separated decimal segments compare as numbers of any size, never as
// floats, and where one version is a prefix of the other the shorter sorts
// first. A version with an empty or non-decimal segment is refused.
func ComparePack(a, b string) (int, error) {
	x, err := packSegments(a)
	if err != nil {
		return 0, err
	}
	y, err := packSegments(b)
	if err != nil {
		return 0, err
	}
	if ra, rb := releaseForm(a), releaseForm(b); ra != rb {
		if ra {
			return 1, nil
		}
		return -1, nil
	}
	for i := 0; i < len(x) || i < len(y); i++ {
		switch {
		case i >= len(x):
			return -1, nil
		case i >= len(y):
			return 1, nil
		}
		if c := compareDecimal(x[i], y[i]); c != 0 {
			return c, nil
		}
	}
	return 0, nil
}

// releaseForm reports whether s is a <major>.<day>.<n> release version.
func releaseForm(s string) bool {
	v, err := Parse(s)
	return err == nil && v != (V{})
}

// ValidPack reports whether s is a pack version ComparePack reads, in
// either form: a published index keeps its older entries.
func ValidPack(s string) bool {
	_, err := packSegments(s)
	return err == nil
}

func packSegments(s string) ([]string, error) {
	parts := strings.Split(s, ".")
	for _, p := range parts {
		if p == "" || strings.TrimLeft(p, "0123456789") != "" {
			return nil, fmt.Errorf("pack version %q: want dot-separated numbers", s)
		}
	}
	return parts, nil
}

func compareDecimal(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	switch {
	case len(a) != len(b):
		if len(a) < len(b) {
			return -1
		}
		return 1
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
