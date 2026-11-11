package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Montage du stockage refait pendant que le launcher tourne : /proc/self/exe ne
// donne plus que le chemin interne au partage (« /CLAUDE_PROJETS/… ») ; la mise
// à jour doit viser le chemin vu au démarrage.
func TestPickExeAfterRemount(t *testing.T) {
	base := t.TempDir()
	start := filepath.Join(base, "CLAUDE_PROJETS", "claude-launcher-ng2")
	_ = mkdirAll(filepath.Dir(start))
	detached := "/CLAUDE_PROJETS-absent-du-test/claude-launcher-ng2"

	if got, err := pickExe([]string{detached, start}); err != nil || got != start {
		t.Fatalf("pickExe = %q, %v ; attendu %q", got, err, start)
	}
	if got, _ := pickExe([]string{start, "/ailleurs/x"}); got != start {
		t.Fatalf("chemin actuel valable ignoré : %q", got)
	}
	if _, err := pickExe([]string{detached, "", "relatif/x"}); err == nil {
		t.Fatal("aucun candidat valable : erreur attendue")
	}
}

func mkdirAll(d string) error { return os.MkdirAll(d, 0o755) }
