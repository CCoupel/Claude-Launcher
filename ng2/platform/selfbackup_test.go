package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeExe(t *testing.T, p, c string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(c), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readStr(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBackupPath(t *testing.T) {
	for in, want := range map[string]string{"v6.5.0": "x.bak-v6.5.0", "6.5.0": "x.bak-v6.5.0", "dev": "", "": "", "v1/../2": "", "v1..2": ""} {
		if got := BackupPath("x", in); got != want {
			t.Errorf("BackupPath(%q) = %q, attendu %q", in, got, want)
		}
	}
}

// Les deux flux (copie Linux, renommage Windows) : sauvegarde créée, nouveau posé, un seul .bak gardé.
func TestSauvegardeUneSeule218(t *testing.T) {
	flows := map[string]func(dst, ver string, w func(string) error) error{
		"copie": replaceWithCopyBackup, "renommage": swapRunningExeBackup,
	}
	for name, flow := range flows {
		t.Run(name, func(t *testing.T) {
			d := t.TempDir()
			exe := filepath.Join(d, "ng.exe")
			writeExe(t, exe, "v1")
			writeExe(t, exe+".bak-v0.9.0", "v0.9") // ancienne sauvegarde : purgée
			err := flow(exe, "v1.0.0", func(p string) error { return os.WriteFile(p, []byte("v2"), 0o755) })
			if err != nil {
				t.Fatal(err)
			}
			if readStr(t, exe) != "v2" || readStr(t, exe+".bak-v1.0.0") != "v1" {
				t.Errorf("exe=%q bak=%q", readStr(t, exe), readStr(t, exe+".bak-v1.0.0"))
			}
			if m, _ := filepath.Glob(exe + ".bak-v*"); len(m) != 1 {
				t.Errorf("sauvegardes = %v, attendu une seule", m)
			}
			if st, _ := os.Stat(exe + ".bak-v1.0.0"); st.Mode().Perm() != 0o755 {
				t.Errorf("droits de la sauvegarde %v", st.Mode().Perm())
			}
		})
	}
}

// Échec d'écriture : l'original est intact (copie) / restauré (renommage), l'ancienne sauvegarde est gardée.
func TestEchecEcritureRestaure218(t *testing.T) {
	boom := errors.New("disque plein")
	for name, flow := range map[string]func(dst, ver string, w func(string) error) error{
		"copie": replaceWithCopyBackup, "renommage": swapRunningExeBackup,
	} {
		t.Run(name, func(t *testing.T) {
			d := t.TempDir()
			exe := filepath.Join(d, "ng.exe")
			writeExe(t, exe, "v1")
			writeExe(t, exe+".bak-v0.9.0", "v0.9")
			err := flow(exe, "v1.0.0", func(p string) error { _ = os.WriteFile(p, []byte("moitié"), 0o755); return boom })
			if !errors.Is(err, boom) {
				t.Fatalf("erreur = %v", err)
			}
			if got := readStr(t, exe); got != "v1" && got != "moitié" || name == "renommage" && got != "v1" {
				t.Errorf("exe = %q après échec", got)
			}
			if _, err := os.Stat(exe + ".bak-v0.9.0"); err != nil {
				t.Error("ancienne sauvegarde purgée malgré l'échec")
			}
		})
	}
}

func TestVersionInutilisableSansSauvegarde218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	writeExe(t, exe, "v1")
	if err := replaceWithCopyBackup(exe, "dev", func(p string) error { return os.WriteFile(p, []byte("v2"), 0o755) }); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(exe + ".bak-*"); len(m) != 0 {
		t.Errorf("sauvegarde créée pour une version dev : %v", m)
	}
}

// keepPath vide ou inconnu : aucune suppression (jamais de purge totale).
func TestPurgeSansCleConnueNeSupprimeRien218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	writeExe(t, exe+".bak-v1.0.0", "a")
	writeExe(t, exe+".bak-v2.0.0", "b")
	for _, keep := range []string{"", exe + ".bak-v9.9.9", "v2.0.0", filepath.Join(d, "autre.bak-v2.0.0")} {
		if left := PurgeBackups(exe, keep); len(left) != 0 {
			t.Errorf("keep=%q : restantes %v", keep, left)
		}
		if m, _ := filepath.Glob(exe + ".bak-v*"); len(m) != 2 {
			t.Fatalf("keep=%q : sauvegardes = %v, attendu les 2", keep, m)
		}
	}
}

// La plus récente se juge à la VERSION (numérique par segments), pas à la date.
func TestNewestBackupParVersion218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	writeExe(t, exe+".bak-v6.9.0", "a")
	writeExe(t, exe+".bak-v6.10.0", "b") // 10 > 9 (pas un ordre alphabétique)
	writeExe(t, exe+".bak-v5.99.9", "c")
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(exe+".bak-v6.10.0", old, old) // la plus récente par version est la plus ancienne par date
	if got := newestBackup(exe); got != exe+".bak-v6.10.0" {
		t.Errorf("newestBackup = %s", got)
	}
	PurgeBackups(exe, newestBackup(exe))
	if m, _ := filepath.Glob(exe + ".bak-v*"); len(m) != 1 || m[0] != exe+".bak-v6.10.0" {
		t.Errorf("restantes = %v", m)
	}
}

// Sauvegarde impossible : mise à jour faite, avertissement visible (TakeBackupWarning), vidé à la lecture.
func TestAvertissementSansSauvegarde218(t *testing.T) {
	d := t.TempDir()
	exe := filepath.Join(d, "ng")
	writeExe(t, exe, "v1")
	if err := replaceWithCopyBackup(exe, "dev", func(p string) error { return os.WriteFile(p, []byte("v2"), 0o755) }); err != nil {
		t.Fatal(err)
	}
	if w := TakeBackupWarning(); w == "" {
		t.Error("avertissement attendu pour une version non sauvegardable")
	}
	if w := TakeBackupWarning(); w != "" {
		t.Errorf("avertissement non vidé : %q", w)
	}
	writeExe(t, exe, "v2")
	if err := replaceWithCopyBackup(exe, "v1.0.0", func(p string) error { return os.WriteFile(p, []byte("v3"), 0o755) }); err != nil {
		t.Fatal(err)
	}
	if w := TakeBackupWarning(); w != "" {
		t.Errorf("avertissement inattendu : %q", w)
	}
}
