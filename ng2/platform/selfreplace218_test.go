package platform

// #218 : sauvegarde du binaire avant remplacement (contracts/desktop-dispatch.md « Sauvegarde avant mise à jour »),
// logique pure, testée sous Linux. Les deux flux — copie atomique (Linux/macOS : replaceWithCopyBackup) et renommage
// (Windows : swapRunningExeBackup) — passent par les mêmes scénarios. PurgeBackups(exe, keep) : keep est le chemin de la sauvegarde à garder.

import (
	"os"
	"path/filepath"
	"testing"
)

var flux218 = map[string]func(dst, version string, write func(string) error) error{
	"copie":     replaceWithCopyBackup,
	"renommage": swapRunningExeBackup,
}

var curFlux218 = replaceWithCopyBackup

func swap218(dst, version string, write func(string) error) error {
	return curFlux218(dst, version, write)
}

// forFlux218 : le scénario est joué avec les deux flux.
func forFlux218(t *testing.T, f func(t *testing.T)) {
	t.Helper()
	for name, fl := range flux218 {
		t.Run(name, func(t *testing.T) {
			old := curFlux218
			curFlux218 = fl
			defer func() { curFlux218 = old }()
			f(t)
		})
	}
}

func put218(t *testing.T, p, s string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), mode); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(p, mode)
}

func read218(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s : %v", p, err)
	}
	return string(b)
}

func writer218(content string) func(string) error {
	return func(dst string) error {
		tmp := dst + ".w"
		if err := os.WriteFile(tmp, []byte(content), 0o755); err != nil {
			return err
		}
		return os.Rename(tmp, dst)
	}
}

func baks218(t *testing.T, exe string) []string {
	t.Helper()
	m, _ := filepath.Glob(globEscape(exe) + ".bak-v*")
	return m
}

func TestBackupExeNom218(t *testing.T) {
	for in, want := range map[string]string{"v6.5.0": "/x/ng.bak-v6.5.0", "6.5.0": "/x/ng.bak-v6.5.0"} {
		if got := BackupPath("/x/ng", in); got != want {
			t.Errorf("BackupPath(%q) = %q, attendu %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "dev", "v../x", "6/5"} { // version inutilisable dans un nom de fichier : pas de sauvegarde
		if got := BackupPath("/x/ng", bad); got != "" {
			t.Errorf("BackupPath(%q) = %q, attendu vide", bad, got)
		}
	}
}

func TestSwapWithBackupCreeLaSauvegardeDeLaVersionCourante218(t *testing.T) {
	forFlux218(t, func(t *testing.T) {
		d := t.TempDir()
		exe := filepath.Join(d, "ng")
		put218(t, exe, "V1", 0o755)
		if err := swap218(exe, "v6.5.0", writer218("V2")); err != nil {
			t.Fatal(err)
		}
		if got := read218(t, exe); got != "V2" {
			t.Errorf("exe = %q, attendu V2", got)
		}
		bak := BackupPath(exe, "v6.5.0")
		if got := read218(t, bak); got != "V1" {
			t.Errorf("sauvegarde = %q, attendu l'ancien binaire V1", got)
		}
		if st, _ := os.Stat(bak); st.Mode().Perm() != 0o755 {
			t.Errorf("droits de la sauvegarde %v, attendu 0755 (exécutable)", st.Mode().Perm())
		}
		if m := baks218(t, exe); len(m) != 1 {
			t.Errorf("sauvegardes = %v, attendu une seule", m)
		}
	})
}

func TestSwapWithBackupNeGardeQuUneSauvegarde218(t *testing.T) {
	forFlux218(t, func(t *testing.T) {
		d := t.TempDir()
		exe := filepath.Join(d, "ng")
		put218(t, exe, "V3", 0o755)
		put218(t, BackupPath(exe, "v6.3.0"), "V1", 0o755)
		put218(t, BackupPath(exe, "v6.4.0"), "V2", 0o755)
		put218(t, exe+".autre", "X", 0o755)                       // pas une sauvegarde : jamais touché
		put218(t, exe+".bak", "Y", 0o644)                         // motif voisin sans « -v » : jamais touché
		put218(t, filepath.Join(d, "ng2.bak-v6.3.0"), "Z", 0o755) // autre exécutable : jamais touché
		if err := swap218(exe, "v6.5.0", writer218("V4")); err != nil {
			t.Fatal(err)
		}
		m := baks218(t, exe)
		if len(m) != 1 || m[0] != BackupPath(exe, "v6.5.0") {
			t.Fatalf("sauvegardes = %v, attendu seulement .bak-v6.5.0", m)
		}
		if read218(t, m[0]) != "V3" {
			t.Errorf("sauvegarde = %q, attendu V3", read218(t, m[0]))
		}
		for _, p := range []string{exe + ".autre", exe + ".bak", filepath.Join(d, "ng2.bak-v6.3.0")} {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("%s supprimé à tort", filepath.Base(p))
			}
		}
	})
}

func TestSwapWithBackupEchecEcritureRestaureLOriginal218(t *testing.T) {
	forFlux218(t, func(t *testing.T) {
		d := t.TempDir()
		exe := filepath.Join(d, "ng")
		put218(t, exe, "ORIGINAL", 0o755)
		ancienne := BackupPath(exe, "v6.4.0")
		put218(t, ancienne, "VIEUX", 0o755)
		err := swap218(exe, "v6.5.0", func(dst string) error {
			_ = os.WriteFile(dst, []byte("MOITIÉ"), 0o755) // fichier à moitié posé
			return os.ErrInvalid
		})
		if err == nil {
			t.Fatal("erreur attendue")
		}
		if got := read218(t, exe); got != "ORIGINAL" {
			t.Errorf("exe = %q, l'original doit être remis en place", got)
		}
		if st, _ := os.Stat(exe); st.Mode().Perm() != 0o755 {
			t.Errorf("droits de l'exe restauré : %v", st.Mode().Perm())
		}
		if got := read218(t, ancienne); got != "VIEUX" {
			t.Errorf("sauvegarde plus ancienne touchée par un échec : %q", got)
		}
		if m, _ := filepath.Glob(globEscape(exe) + ".*.w"); len(m) != 0 {
			t.Errorf("temporaires laissés : %v", m)
		}
	})
}

func TestSwapWithBackupPremiereInstallationSansExe218(t *testing.T) {
	forFlux218(t, func(t *testing.T) {
		d := t.TempDir()
		exe := filepath.Join(d, "ng") // rien à sauvegarder
		if err := swap218(exe, "v6.5.0", writer218("V1")); err != nil {
			t.Fatal(err)
		}
		if read218(t, exe) != "V1" || len(baks218(t, exe)) != 0 {
			t.Errorf("exe=%q sauvegardes=%v", read218(t, exe), baks218(t, exe))
		}
	})
}

func TestSwapWithBackupMemeVersionRemplaceLaSauvegarde218(t *testing.T) {
	forFlux218(t, func(t *testing.T) {
		d := t.TempDir()
		exe := filepath.Join(d, "ng")
		put218(t, exe, "V2", 0o755)
		put218(t, BackupPath(exe, "v6.5.0"), "ANCIENNE-6.5.0", 0o755) // réinstallation de la même version
		if err := swap218(exe, "v6.5.0", writer218("V3")); err != nil {
			t.Fatal(err)
		}
		if got := read218(t, BackupPath(exe, "v6.5.0")); got != "V2" {
			t.Errorf("sauvegarde = %q, attendu V2 (binaire remplacé)", got)
		}
		if len(baks218(t, exe)) != 1 {
			t.Errorf("sauvegardes = %v", baks218(t, exe))
		}
	})
}

// « verrouillée » sous Linux : un dossier non vide portant le nom d'une sauvegarde ne peut pas être supprimé par
// os.Remove ; au démarrage suivant, PurgeBackups la retente.
func TestPurgeBackupsRetenteLesVerrouillees218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	put218(t, exe, "V", 0o755)
	put218(t, BackupPath(exe, "v6.5.0"), "COURANTE", 0o755)
	put218(t, BackupPath(exe, "v6.3.0"), "LIBRE", 0o755)
	verrou := BackupPath(exe, "v6.4.0")
	if err := os.Mkdir(verrou, 0o755); err != nil {
		t.Fatal(err)
	}
	put218(t, filepath.Join(verrou, "tenu"), "x", 0o644)
	left := PurgeBackups(exe, BackupPath(exe, "v6.5.0"))
	if len(left) != 1 || left[0] != verrou {
		t.Fatalf("restantes = %v, attendu la sauvegarde verrouillée %s", left, verrou)
	}
	if _, err := os.Stat(BackupPath(exe, "v6.3.0")); err == nil {
		t.Error("sauvegarde libre non supprimée")
	}
	if read218(t, BackupPath(exe, "v6.5.0")) != "COURANTE" {
		t.Error("sauvegarde à garder supprimée")
	}
	// le verrou saute : au démarrage suivant, plus rien ne reste
	_ = os.RemoveAll(verrou)
	if left := PurgeBackups(exe, BackupPath(exe, "v6.5.0")); len(left) != 0 {
		t.Errorf("après déverrouillage : %v", left)
	}
	if left := PurgeBackups(filepath.Join(d, "absent"), ""); len(left) != 0 { // rien : sans erreur
		t.Errorf("exe sans sauvegarde : %v", left)
	}
}

// Sémantique tranchée (review) : keep vide (ou version inutilisable) = AUCUNE suppression.
func TestPurgeBackupsKeepVideNeSupprimeRien218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	put218(t, exe, "V", 0o755)
	a, b := BackupPath(exe, "v6.3.0"), BackupPath(exe, "v6.4.0")
	put218(t, a, "A", 0o755)
	put218(t, b, "B", 0o755)
	for _, keep := range []string{"", "dev", "v../x"} {
		if left := PurgeBackups(exe, keep); len(left) != 0 {
			t.Errorf("keep=%q : restantes %v", keep, left)
		}
		for _, p := range []string{a, b} {
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("keep=%q : %s supprimée (purge totale)", keep, filepath.Base(p))
			}
		}
	}
	if left := PurgeBackups(filepath.Join(d, "absent"), ""); len(left) != 0 { // exe sans sauvegarde : sans effet
		t.Errorf("exe absent : %v", left)
	}
}
