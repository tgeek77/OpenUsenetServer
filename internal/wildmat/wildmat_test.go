package wildmat

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"abc", "abc", true},
		{"abc", "ab", false},
		{"a*", "abc", true},
		{"a*b", "axb", true},
		{"a*,!*b", "aaa", true},
		{"a*,!*b", "abb", false},
		{"a*,!*b,*c*", "ccb", true},
		{"*", "local.test", true},
		{"local.*", "local.test", true},
		{"local.*", "alt.test", false},
		{"*,!local.*", "alt.test", true},
		{"*,!local.*", "local.test", false},
		{"*,@*.bina*,!local.*", "alt.binaries.foo", false},
		{"*,@*.bina*,!local.*", "comp.lang.go", true},
	}
	for _, c := range cases {
		got := Match(c.pat, c.s)
		if got != c.want {
			t.Errorf("Match(%q,%q)=%v want %v", c.pat, c.s, got, c.want)
		}
	}
}
