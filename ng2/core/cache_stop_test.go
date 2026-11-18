package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Arrêt du gestionnaire d'état pendant une rafale d'évènements de fichiers, appelé en même temps de plusieurs endroits
// (App.Stop et le nettoyage d'un test) : ne se fige jamais (CI Windows : interblocage à l'arrêt, Close du surveillant
// bloqué sur un envoi d'évènement sans lecteur) et reste sans course (go test -race).
func TestStateMgrStopPendantUneRafale(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}
	t.Setenv(CacheDirVar, t.TempDir())
	proj := filepath.Join(t.TempDir(), "p")
	_ = os.MkdirAll(proj, 0o755)
	if out, err := exec.Command("git", "-C", proj, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init : %v %s", err, out)
	}
	m, err := NewStateMgr()
	if err != nil {
		t.Fatal(err)
	}
	m.SetProjects([]string{proj})
	// rafale d'évènements dans le projet surveillé
	stopWrites := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stopWrites:
				return
			default:
			}
			_ = os.WriteFile(filepath.Join(proj, "f"+strconv.Itoa(i%50)+".txt"), []byte("x"), 0o644)
		}
	}()
	time.Sleep(200 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		var sw sync.WaitGroup
		for i := 0; i < 4; i++ { // plusieurs arrêts simultanés : sans effet au-delà du premier
			sw.Add(1)
			go func() { defer sw.Done(); m.Stop() }()
		}
		sw.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("StateMgr.Stop figé pendant une rafale d'évènements")
	}
	close(stopWrites)
	wg.Wait()
	m.Stop() // et encore après coup
}
