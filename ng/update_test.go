package main

import "testing"

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v2.28.0", "v2.29.0", true},
		{"v2.29.0", "v2.28.0", false},
		{"v2.28.0", "v2.28.0", false},
		{"v2.9.0", "v2.10.0", true}, // comparaison numérique, pas lexicale
		{"v2.28.0", "v3.0.0", true},
		{"", "v1.0.0", true},
		{"v2.28.0", "", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q,%q)=%v, attendu %v", c.a, c.b, got, c.want)
		}
	}
}
