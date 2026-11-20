package core

// #218 : InstallBinary sauvegarde le binaire courant en <exe>.bak-v<version courante> (contrat « Sauvegarde avant mise à
// jour »). Réutilise platform.BackupPath / PurgeBackups.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"claude-launcher-ng2/platform"
)

func withVersion218(t *testing.T, v string) {
	t.Helper()
	old := Version()
	SetNgVersion(v)
	t.Cleanup(func() { SetNgVersion(old) })
}

func TestInstallBinaryGardeLAncienBinaireEnBak218(t *testing.T) {
	withVersion218(t, "v6.5.0")
	d := t.TempDir()
	dst := filepath.Join(d, "claude-launcher-ng2")
	if err := os.WriteFile(dst, []byte("ANCIEN"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallBinary(dst, []byte("NOUVEAU")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "NOUVEAU" {
		t.Errorf("binaire = %q", b)
	}
	bak := platform.BackupPath(dst, "v6.5.0")
	if b, err := os.ReadFile(bak); err != nil || string(b) != "ANCIEN" {
		t.Errorf("sauvegarde %s = %q, %v", filepath.Base(bak), b, err)
	}
	if runtime.GOOS != "windows" { // bits d'exécution : notion Unix
		if st, _ := os.Stat(dst); st.Mode().Perm() != 0o755 {
			t.Errorf("droits = %v", st.Mode().Perm())
		}
	}
	// mise à jour suivante (la version courante est maintenant 6.6.0) : une seule sauvegarde, la dernière
	SetNgVersion("v6.6.0")
	if err := InstallBinary(dst, []byte("NOUVEAU2")); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(dst + ".bak-v*")
	if len(m) != 1 || m[0] != platform.BackupPath(dst, "v6.6.0") {
		t.Fatalf("sauvegardes = %v, attendu seulement .bak-v6.6.0", m)
	}
	if b, _ := os.ReadFile(m[0]); string(b) != "NOUVEAU" {
		t.Errorf("sauvegarde = %q, attendu NOUVEAU (binaire remplacé)", b)
	}
}

func TestInstallBinaryPremiereInstallationSansBak218(t *testing.T) {
	withVersion218(t, "v6.5.0")
	dst := filepath.Join(t.TempDir(), "claude-launcher-ng2")
	if err := InstallBinary(dst, []byte("X")); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(dst + ".bak-v*"); len(m) != 0 {
		t.Errorf("sauvegarde sans binaire précédent : %v", m)
	}
}

// Échec portable : dossier de destination absent (CreateTemp échoue sous tous les systèmes) — erreur, aucune
// sauvegarde ni dossier créés. L'échec d'écriture avec restauration de l'original est couvert en logique pure
// (injection de write) par platform/selfreplace218_test.go.
func TestInstallBinaryDossierAbsentEchoueProprement218(t *testing.T) {
	withVersion218(t, "v6.5.0")
	d := t.TempDir()
	dst := filepath.Join(d, "absent", "claude-launcher-ng2")
	if err := InstallBinary(dst, []byte("NOUVEAU")); err == nil {
		t.Fatal("erreur attendue")
	}
	if ents, _ := os.ReadDir(d); len(ents) != 0 {
		t.Errorf("fichiers ou dossiers créés malgré l'échec : %v", ents)
	}
}
