package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// #176 revue M1 : la libération ne supprime pas le verrou d'un autre (détenteur lent dont le verrou a été repris).
func TestLockReleaseKeepsForeignLock(t *testing.T) {
	target := filepath.Join(t.TempDir(), "f.json")
	lock := target + ".lock"
	err := WithFileLock(target, func() error {
		// pendant la section, le verrou est repris par un autre (péremption) : le fichier porte un autre identifiant
		if err := os.WriteFile(lock, []byte("autre-id launcher\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, e := os.ReadFile(lock); e != nil || string(b) != "autre-id launcher\n" {
		t.Errorf("le verrou d'un autre a été supprimé ou modifié à la libération : %q %v", b, e)
	}
}

// Reprise d'un verrou périmé : plusieurs candidats, un seul entre à la fois (aucun double détenteur).
func TestStaleLockReclaimedByOneHolderAtATime(t *testing.T) {
	target := filepath.Join(t.TempDir(), "f.json")
	lock := target + ".lock"
	if err := os.WriteFile(lock, []byte("mort\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * fileLockStale.Get())
	_ = os.Chtimes(lock, old, old)
	var inside, maxInside, done atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithFileLock(target, func() error {
				n := inside.Add(1)
				for {
					m := maxInside.Load()
					if n <= m || maxInside.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inside.Add(-1)
				done.Add(1)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maxInside.Load() != 1 || done.Load() != 8 {
		t.Errorf("détenteurs simultanés max %d, sections faites %d/8", maxInside.Load(), done.Load())
	}
	if _, err := os.Stat(lock); err == nil {
		t.Error("verrou laissé")
	}
}

// M2 : liste des clés settings-env par launcher quand CL_HUB_NAME est posé ; fichier historique sinon.
func TestSettingsEnvKeysPathPerLauncher(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CL_HUB_NAME", "")
	if p := settingsEnvKeysPath(dir); filepath.Base(p) != "settings-env.json" {
		t.Errorf("sans CL_HUB_NAME : %q", p)
	}
	t.Setenv("CL_HUB_NAME", "pod-a")
	if p := settingsEnvKeysPath(dir); filepath.Base(p) != "settings-env-pod-a.json" {
		t.Errorf("avec CL_HUB_NAME : %q", p)
	}
}

// Revue M1/M2 : un verrou périmé qui ne peut pas être retiré (dossier sans droit d'écriture) ne fait jamais boucler
// sans fin : erreur explicite « droits insuffisants », durée bornée par l'échéance.
func TestStaleLockUnremovableGivesBoundedExplicitError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("exige un dossier non modifiable (non root, hors Windows)")
	}
	oldS, oldT := fileLockStale.Get(), fileLockTimeout.Get()
	fileLockStale.Set(50 * time.Millisecond)
	fileLockTimeout.Set(300 * time.Millisecond)
	defer func() { fileLockStale.Set(oldS); fileLockTimeout.Set(oldT) }()
	dir := t.TempDir()
	target := filepath.Join(dir, "f.json")
	lock := target + ".lock"
	if err := os.WriteFile(lock, []byte("mort\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(lock, old, old)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	ran := false
	t0 := time.Now()
	err := WithFileLock(target, func() error { ran = true; return nil })
	if err == nil || ran {
		t.Fatalf("erreur attendue, section exécutée=%v err=%v", ran, err)
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("attente non bornée : %v", d)
	}
	if !strings.Contains(err.Error(), "droits") {
		t.Errorf("l'erreur doit dire « droits insuffisants » : %v", err)
	}
}
