package core

import (
	"strings"
	"testing"
	"time"
)

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
		if got := VersionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q,%q)=%v, attendu %v", c.a, c.b, got, c.want)
		}
	}
}

// le template n'est jamais pris dans une majeure supérieure à celle du launcher
func TestBestTagUpTo(t *testing.T) {
	mk := func(names ...string) []tagRef {
		out := make([]tagRef, len(names))
		for i, n := range names {
			out[i].Name = n
		}
		return out
	}
	tags := mk("v6.0.0", "v4.3.1", "v5.1.0", "v5.0.2", "latest", "v5.10.0")
	for _, c := range []struct {
		major int
		want  string
	}{{5, "v5.10.0"}, {6, "v6.0.0"}, {4, "v4.3.1"}, {3, ""}, {0, "v6.0.0"}} {
		got := ""
		if i := bestTagUpTo(tags, c.major); i >= 0 {
			got = tags[i].Name
		}
		if got != c.want {
			t.Errorf("majeure %d : %q, attendu %q", c.major, got, c.want)
		}
	}
}

func TestProgressLine(t *testing.T) {
	if got := progressLine("X", 3, 8, 2*time.Second); got != "  X  █████████░░░░░░░░░░░░░░░  3/8  2s" {
		t.Fatalf("%q", got)
	}
	if got := progressLine("X", 0, 0, 0); !strings.Contains(got, "0/0") {
		t.Fatalf("%q", got)
	}
}
