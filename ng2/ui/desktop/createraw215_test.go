//go:build !windows

package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

// `raw` reçu par create est IGNORÉ (contrat) : jamais de CLAUDE.md minimal ; Projet brut = dossier + git init.
func TestCreateIgnoreRaw215(t *testing.T) {
	f := newFix215(t, "")
	for i, raw := range []bool{true, false} {
		name := []string{"brutA", "brutB"}[i]
		dir := filepath.Join(f.base, "TEST", name)
		v := f.create(t, map[string]any{"base": f.base, "category": "TEST", "name": name, "source": "empty", "raw": raw})
		if v["tab"] != dir {
			t.Fatalf("create(raw=%v) = %v", raw, v)
		}
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
			t.Errorf("raw=%v : CLAUDE.md minimal créé (raw doit être sans effet)", raw)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Errorf("raw=%v : pas de git init", raw)
		}
		if coreMarker215(dir) {
			t.Errorf("raw=%v : marqueur init-proposed pour un projet brut", raw)
		}
	}
}

// Remote refusé par create : {ok:false, field:"remote"} avant toute écriture (pas de dossier créé).
func TestCreateRefuseUnRemoteDangereux215(t *testing.T) {
	f := newFix215(t, "")
	for i, bad := range []string{"--upload-pack=x", "ext::sh -c id", "https://u:p@github.com/a/b", "a b"} {
		name := "rem" + string(rune('a'+i))
		v := f.create(t, map[string]any{"base": f.base, "category": "TEST", "name": name, "source": "template", "remote": bad})
		if v["ok"] != false || v["field"] != "remote" {
			t.Errorf("remote %q : %v", bad, v)
		}
		if _, err := os.Stat(filepath.Join(f.base, "TEST", name)); err == nil {
			t.Errorf("remote %q : dossier créé malgré le refus", bad)
		}
	}
}
