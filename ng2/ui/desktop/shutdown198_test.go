package desktop

import (
	"claude-launcher-ng2/core"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// À l'arrêt (SIGTERM), le launcher attend la fin d'une ouverture de projet en cours, dans la limite du délai de grâce (#198).
func TestArretAttendLesOuvertures(t *testing.T) {
	var r recorder
	a := newApp(t, r.emit)
	if !a.waitOpenings(50 * time.Millisecond) {
		t.Error("aucune ouverture : rendu immédiat attendu")
	}
	a.openGate.begin()
	go func() { time.Sleep(300 * time.Millisecond); a.openGate.end() }()
	start := time.Now()
	if !a.waitOpenings(3 * time.Second) {
		t.Fatal("ouverture finie dans le délai : l'attente doit réussir")
	}
	if d := time.Since(start); d < 250*time.Millisecond || d > 2*time.Second {
		t.Errorf("attente = %v, attendu ≈ 300 ms", d)
	}
}

// Ouverture qui ne finit pas : l'arrêt n'attend pas plus que le délai (les terminaux sont détachés, le bootstrap du
// conteneur étant détaché du launcher il se termine sans lui).
func TestArretNAttendPasAuDela(t *testing.T) {
	var r recorder
	a := newApp(t, r.emit)
	a.openGate.begin()
	defer a.openGate.end()
	start := time.Now()
	if a.waitOpenings(200 * time.Millisecond) {
		t.Fatal("ouverture non finie : l'attente doit expirer")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("attente = %v, attendu ≈ 200 ms", d)
	}
}

// Stop attend les ouvertures (shutdownOpenWait) puis détache.
func TestStopAttendUneOuvertureEnCours(t *testing.T) {
	old := shutdownOpenWait.Get()
	shutdownOpenWait.Set(2 * time.Second)
	defer func() { shutdownOpenWait.Set(old) }()
	var r recorder
	a := newApp(t, r.emit)
	a.openGate.begin()
	go func() { time.Sleep(400 * time.Millisecond); a.openGate.end() }()
	start := time.Now()
	a.Stop()
	if d := time.Since(start); d < 350*time.Millisecond {
		t.Errorf("Stop n'a pas attendu l'ouverture en cours (%v)", d)
	}
}

// begin / end / wait concurrents : pas de course (go test -race), le compteur retombe à zéro.
func TestOpenGateConcurrent(t *testing.T) {
	var g openGate
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			g.begin()
			time.Sleep(time.Millisecond)
			g.end()
			done <- struct{}{}
		}()
		go func() { g.wait(time.Millisecond) }()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	if !g.wait(time.Second) {
		t.Error("compteur non retombé à zéro")
	}
}

// Un détachement bloqué (client tmux qui ne se ferme pas) ne retient pas l'arrêt au-delà de la borne : budget
// d'arrêt = ouvertures (20 s) + détachements (≤ 5 s), sous le délai de grâce du pod (30 s).
func TestArretNAttendPasUnDetachBloque(t *testing.T) {
	defer detachWait.Set(detachWait.Set(200 * time.Millisecond))
	var r recorder
	a := newApp(t, r.emit)
	n := core.Node{Name: "alpha", Path: t.TempDir(), Project: true, Key: "alpha"}
	term := newProjTerm(a, n, core.LaunchNormal)
	a.mu.Lock()
	a.terms[n.Path] = term
	a.mu.Unlock()
	term.mu.Lock() // detach() attend ce verrou : bloqué tant que le test ne le rend pas
	defer func() { term.mu.Unlock() }()
	start := time.Now()
	a.Stop()
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Stop retenu par un détachement bloqué : %v (borne 200 ms)", d)
	}
}

func TestWaitDone(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	if waitDone(&wg, 100*time.Millisecond) {
		t.Error("groupe non fini : faux attendu")
	}
	wg.Done()
	if !waitDone(&wg, time.Second) {
		t.Error("groupe fini : vrai attendu")
	}
}

// projectCount (Start) et rescan (arborescence surveillée, Ctrl+R) tournent dans des goroutines différentes : pas de course
// sur la liste des projets (go test -race ; CI ubuntu : App.rescan écrivait pendant qu'App.Start lisait).
func TestProjectCountEtRescanConcurrents(t *testing.T) {
	ps := sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	root := filepath.Dir(ps[0])
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			d := filepath.Join(root, "nouveau"+strconv.Itoa(i))
			_ = os.MkdirAll(filepath.Join(d, ".git"), 0o755)
			a.rescan()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			_ = a.projectCount()
		}
	}()
	wg.Wait()
	if n := a.projectCount(); n < 2 {
		t.Errorf("projets connus : %d", n)
	}
}
