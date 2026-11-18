package desktop

import (
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
