package core

import (
	"github.com/fsnotify/fsnotify"
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

// drainClose : un Close qui ne rend la main que si Events est lu (surveillant Windows) finit ; un Close qui ne rend jamais
// la main n'empêche pas l'arrêt au-delà du délai.
func TestDrainClose(t *testing.T) {
	// Close bloqué tant que l'évènement en vol n'est pas lu
	events := make(chan fsnotify.Event)
	errs := make(chan error)
	closeFn := func() error { events <- fsnotify.Event{Name: "x"}; return nil } // l'envoi attend un lecteur
	start := time.Now()
	if !drainClose(closeFn, events, errs, 5*time.Second, "test") || time.Since(start) > 2*time.Second {
		t.Errorf("Close bloqué sur un envoi : non fini (%v)", time.Since(start))
	}
	// Close qui ne rend jamais la main : borné
	block := make(chan struct{})
	defer close(block)
	start = time.Now()
	if drainClose(func() error { <-block; return nil }, events, errs, 150*time.Millisecond, "test") || time.Since(start) > 3*time.Second {
		t.Errorf("Close sans réponse : non borné (%v)", time.Since(start))
	}
	// vrai surveillant : fermeture immédiate, deux fois sans effet
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	if !closeFsWatcher(w, "test") || !closeFsWatcher(nil, "test") {
		t.Error("fermeture d'un vrai surveillant")
	}
}

// WatchTreeUntil : l'arrêt (done fermé) ferme le surveillant sans se figer, même pendant des évènements.
func TestWatchTreeUntilArretSousEvenements(t *testing.T) {
	root := t.TempDir()
	done := make(chan struct{})
	c := WatchTreeUntil([]string{root}, done)
	stopWrites := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := 0; ; i++ {
			select {
			case <-stopWrites:
				return
			default:
			}
			_ = os.MkdirAll(filepath.Join(root, "g"+strconv.Itoa(i%20)), 0o755)
			_ = os.Remove(filepath.Join(root, "g"+strconv.Itoa((i+7)%20)))
		}
	}()
	select {
	case <-c:
	case <-time.After(3 * time.Second):
	}
	close(done)
	time.Sleep(300 * time.Millisecond)
	close(stopWrites)
	<-finished
}
