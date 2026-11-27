package core

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// LOT-3.2 Batch 0 (#275, E3) — sondage des backends d'utilisateurs (contracts/user-backends.md §3 « Limites », §10) : UN appel en vol par
// backend, attente croissante 10 s → 5 min quand il est injoignable, goroutines bornées. Étiquette « s275api » ; API proposée :
//
//	func NextProbeDelay(consecutiveFailures int) time.Duration                // 0 échec : 10 s ; puis ×2 jusqu'à 5 min
//	type Prober struct{ … } ; func NewProber(now func() time.Time) *Prober   // horloge injectée
//	func (p *Prober) Due(backend string) bool                                // l'attente est écoulée
//	func (p *Prober) Run(backend string, probe func() error) bool            // faux si un appel est déjà en vol ou pas dû ; sinon lance, rend vrai
//	func (p *Prober) Wait()                                                  // attend les appels lancés (tests)

func TestE3AttenteCroissante(t *testing.T) {
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 300 * time.Second, 300 * time.Second}
	for i, w := range want {
		if got := NextProbeDelay(i); got != w {
			t.Errorf("après %d échec(s) : %v, attendu %v", i, got, w)
		}
	}
	if NextProbeDelay(1000) != 5*time.Minute || NextProbeDelay(-1) != 10*time.Second {
		t.Error("bornes : 5 min au plus, 10 s au moins")
	}
}

func TestE3UnSeulAppelEnVolParBackend(t *testing.T) {
	now := time.Now()
	p := NewProber(func() time.Time { return now })
	release := make(chan struct{})
	var calls atomic.Int64
	if !p.Run("b1", func() error { calls.Add(1); <-release; return nil }) {
		t.Fatal("premier sondage non lancé")
	}
	for i := 0; i < 20; i++ { // des relectures de 10 s rapprochées : aucun second appel pour ce backend
		if p.Run("b1", func() error { calls.Add(1); return nil }) {
			t.Fatal("second appel lancé alors que le premier est en vol")
		}
	}
	if !p.Run("b2", func() error { calls.Add(1); return nil }) { // un autre backend n'est pas gêné
		t.Error("un autre backend est bloqué")
	}
	close(release)
	p.Wait()
	if calls.Load() != 2 {
		t.Errorf("%d appels (2 attendus)", calls.Load())
	}
}

func TestE3BackendInjoignableAttenteEtReprise(t *testing.T) {
	now := time.Date(2026, 11, 27, 15, 0, 0, 0, time.UTC)
	p := NewProber(func() time.Time { return now })
	fail := true
	probe := func() error {
		if fail {
			return errHostRefused
		}
		return nil
	}
	started := func() bool { ok := p.Run("b", probe); p.Wait(); return ok }
	if !started() {
		t.Fatal("premier sondage")
	}
	for i, wait := range []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second} {
		now = now.Add(wait - time.Second)
		if p.Due("b") || started() {
			t.Errorf("échec %d : sondage relancé %v après (attente %v)", i+1, wait-time.Second, wait)
		}
		now = now.Add(time.Second)
		if !p.Due("b") || !started() {
			t.Errorf("échec %d : sondage non relancé après %v", i+1, wait)
		}
	}
	fail = false // le backend revient : on repasse à la cadence de 10 s
	now = now.Add(80 * time.Second)
	if !started() {
		t.Fatal("reprise")
	}
	now = now.Add(10 * time.Second)
	if !p.Due("b") {
		t.Error("après un succès : cadence de 10 s attendue")
	}
}

// Goroutines bornées : 200 backends sondés ne créent pas 200 goroutines simultanées (plafond fixe), et rien ne fuit après Wait.
func TestE3GoroutinesBornees(t *testing.T) {
	p := NewProber(time.Now)
	var cur, max atomic.Int64
	var mu sync.Mutex
	gate := make(chan struct{})
	for i := 0; i < 200; i++ {
		name := "b" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		p.Run(name, func() error {
			n := cur.Add(1)
			mu.Lock()
			if n > max.Load() {
				max.Store(n)
			}
			mu.Unlock()
			<-gate
			cur.Add(-1)
			return nil
		})
	}
	time.Sleep(50 * time.Millisecond)
	if max.Load() > 16 {
		t.Errorf("%d sondages simultanés (plafond de 16 au plus)", max.Load())
	}
	close(gate)
	p.Wait()
	if cur.Load() != 0 {
		t.Errorf("%d sondages restés actifs", cur.Load())
	}
}
