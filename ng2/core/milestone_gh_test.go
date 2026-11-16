package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// #158 (GATE LOT-2) : l'erreur d'une lecture GitHub dit la cause réelle (stderr de gh),
// et les cas légitimes (gh absent, non connecté, pas de dépôt GitHub) sont reconnus.

func fakeGH(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux gh en shell : hors Windows")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestReadMilestonesErrorTellsTheCause(t *testing.T) {
	cases := []struct {
		name, script, want, kind string
		benign                   bool
	}{
		{"non connecté", `echo "To get started with GitHub CLI, please run:  gh auth login" >&2; exit 4`, "gh auth login", GHAuth, true},
		{"jeton refusé", `echo "gh: Bad credentials (HTTP 401)" >&2; exit 1`, "Bad credentials", GHAuth, true},
		{"pas de dépôt GitHub", `echo "none of the git remotes configured for this repository point to a known GitHub host" >&2; exit 1`, "known GitHub host", GHNoRepo, true},
		{"erreur réseau", `echo "error connecting to api.github.com" >&2; exit 1`, "api.github.com", GHOther, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeGH(t, c.script)
			_, err := ReadMilestones(t.TempDir(), "")
			if err == nil {
				t.Fatal("erreur attendue")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("message = %q, attendu la cause réelle %q (et non « exit status N »)", err, c.want)
			}
			kind, benign := GHKind(err)
			if kind != c.kind || benign != c.benign {
				t.Errorf("GHKind = %q, %v ; attendu %q, %v", kind, benign, c.kind, c.benign)
			}
		})
	}
}

func TestReadMilestonesGHAbsent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH vidé en shell : hors Windows")
	}
	t.Setenv("PATH", t.TempDir())
	_, err := ReadMilestones(t.TempDir(), "")
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if kind, benign := GHKind(err); kind != GHAbsent || !benign {
		t.Errorf("GHKind = %q, %v ; attendu %q, true (%v)", kind, benign, GHAbsent, err)
	}
	if !strings.Contains(err.Error(), "gh") {
		t.Errorf("message = %q, attendu qu'il nomme gh", err)
	}
}
