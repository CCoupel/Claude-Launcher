package desktop

import (
	"sync/atomic"
	"testing"
	"time"
)

// #210 (dev) : un signal reçu pendant une relecture lente n'est pas perdu ; N signaux = une reprise ; jamais deux relectures
// en même temps ; l'emitter est démarré ici (la fixture de newApp ne le démarre pas).
func TestDev210SignalPendantRelectureLente(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	go a.emitter()
	defer a.Stop()
	var reads, active, maxActive atomic.Int32
	release := make(chan struct{})
	first := make(chan struct{}, 1)
	old := cardsRead.Get()
	cardsRead.Set(func(app *App, path string) Cards {
		n := active.Add(1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		defer active.Add(-1)
		if reads.Add(1) == 1 {
			first <- struct{}{}
			<-release // première relecture bloquée (carte lente)
		}
		return Cards{Path: path, Name: "relu"}
	})
	defer cardsRead.Set(old)
	a.cardsChanged()
	<-first
	for i := 0; i < 5; i++ { // cinq signaux pendant la relecture lente (le canal en garde un, l'emitter le passe à requestCards)
		a.cardsChanged()
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	deadline := time.Now().Add(30 * time.Second)
	for reads.Load() < 2 || func() bool { a.cardsSt.Lock(); defer a.cardsSt.Unlock(); return a.cardsRun }() {
		if time.Now().After(deadline) {
			t.Fatalf("relectures : %d, en cours attendue terminée", reads.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := reads.Load(); n != 2 {
		t.Errorf("%d relectures, attendu 2 (la première + UNE reprise pour les 5 signaux)", n)
	}
	if maxActive.Load() != 1 {
		t.Errorf("relectures simultanées : %d", maxActive.Load())
	}
}
