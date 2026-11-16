package core

import (
	"os"
	"path/filepath"
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
	old := time.Now().Add(-2 * fileLockStale)
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
