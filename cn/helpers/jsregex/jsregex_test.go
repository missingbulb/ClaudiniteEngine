package jsregex

import "testing"

func TestCompileFlags(t *testing.T) {
	for _, c := range []struct {
		src, s string
		want   bool
	}{
		{"/ab/i", "xABy", true},
		{"/^b$/m", "a\nb\nc", true},
		{"/^b$/", "a\nb\nc", false},
		{"/a.b/s", "a\nb", true},
		{"/a.b/", "a\nb", false},
		{"/a[.]b/s", "a\nb", false},
		{"/(?<=x)y/g", "xy", true},
	} {
		body, flags, ok := Form(c.src)
		if !ok {
			t.Fatalf("%s is not a regex form", c.src)
		}
		re, err := Compile(body, flags)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if got := re.Test(c.s); got != c.want {
			t.Errorf("%s on %q: %v, want %v", c.src, c.s, got, c.want)
		}
	}
	if _, _, ok := Form("not a regex"); ok {
		t.Error("a bare string read as a regex form")
	}
}

func TestTrimIsJavaScriptWhitespace(t *testing.T) {
	if got := Trim("\u00a0" + string(rune(0xfeff)) + " x \u2028"); got != "x" {
		t.Errorf("trimmed %q", got)
	}
}
