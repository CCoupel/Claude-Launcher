package platform

// #218 : nettoyage au démarrage (CleanupBackups) : ne garde que la sauvegarde de la plus haute VERSION (comparaison
// numérique, pas la date ni l'ordre alphabétique). Partie
// synchrone de CleanupBackups, sans SelfExe :
//
//	func cleanupBackupsFor(exe string, tries int, pause time.Duration) []string  // rend les sauvegardes qui restent (verrouillées) après tries essais

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupBackupsGardeLaVersionLaPlusHaute218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	put218(t, exe, "V", 0o755)
	vers := []string{"v6.3.0", "v6.9.1", "v6.10.0", "v6.4.2"}
	old := time.Now().Add(-48 * time.Hour)
	for i, v := range vers {
		p := BackupPath(exe, v)
		put218(t, p, v, 0o755)
		// la plus récente par DATE est v6.3.0 : la date ne doit pas décider
		mt := old.Add(time.Duration(len(vers)-i) * time.Hour)
		_ = os.Chtimes(p, mt, mt)
	}
	if left := cleanupBackupsFor(exe, 1, time.Millisecond); len(left) != 0 {
		t.Fatalf("restantes = %v", left)
	}
	m := baks218(t, exe)
	if len(m) != 1 || m[0] != BackupPath(exe, "v6.10.0") {
		t.Errorf("sauvegardes = %v, attendu seulement .bak-v6.10.0 (6.10 > 6.9, comparaison numérique)", m)
	}
}

func TestCleanupBackupsUneOuAucuneSauvegardeIntacte218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	if left := cleanupBackupsFor(exe, 1, time.Millisecond); len(left) != 0 {
		t.Errorf("aucune sauvegarde : %v", left)
	}
	put218(t, BackupPath(exe, "v6.4.0"), "A", 0o755)
	if left := cleanupBackupsFor(exe, 1, time.Millisecond); len(left) != 0 || len(baks218(t, exe)) != 1 {
		t.Errorf("une sauvegarde : restantes=%v présentes=%v", left, baks218(t, exe))
	}
}

func TestCleanupBackupsRetenteLaVerrouillee218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	put218(t, BackupPath(exe, "v6.5.0"), "COURANTE", 0o755)
	verrou := BackupPath(exe, "v6.1.0") // dossier non vide : os.Remove échoue (« verrouillée »)
	_ = os.Mkdir(verrou, 0o755)
	put218(t, filepath.Join(verrou, "tenu"), "x", 0o644)
	left := cleanupBackupsFor(exe, 3, time.Millisecond)
	if len(left) != 1 || left[0] != verrou {
		t.Fatalf("restantes = %v, attendu %s", left, verrou)
	}
	if read218(t, BackupPath(exe, "v6.5.0")) != "COURANTE" {
		t.Error("sauvegarde à garder supprimée")
	}
	_ = os.RemoveAll(verrou) // le verrou saute : l'essai suivant aboutit
	if left := cleanupBackupsFor(exe, 3, time.Millisecond); len(left) != 0 {
		t.Errorf("après déverrouillage : %v", left)
	}
}
