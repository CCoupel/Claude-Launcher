package core

import "testing"

// LOT-3.2 Batch 0 (#275, D5 / T26) — plafonds du launcher PAR UTILISATEUR : ≤ 40 processus d'exec simultanés, ≤ 4 flux /files ; au-delà refus
// nommé ; compteurs libérés à la fermeture. API proposée :
//
//	var ErrGateExec, ErrGateStreams error
//	func NewUserGate() *UserGate
//	func (g *UserGate) AcquireExec(uid string) (release func(), err error)
//	func (g *UserGate) AcquireStream(uid string) (release func(), err error)
//
// T10 (propriétaire) : func (e Engine) VerifyOwner(uid, object string) error — lit le label cl.user de l'objet (inspect) AVANT tout usage ;
// ErrNotOwner si le label diffère ou manque.

func TestD5Plafond40Exec(t *testing.T) {
	g := NewUserGate()
	var rel []func()
	for i := 0; i < 40; i++ {
		r, err := g.AcquireExec("github-1")
		if err != nil {
			t.Fatalf("exec %d/40 refusé : %v", i+1, err)
		}
		rel = append(rel, r)
	}
	if _, err := g.AcquireExec("github-1"); err != ErrGateExec {
		t.Errorf("41e exec : %v (ErrGateExec attendu)", err)
	}
	if r, err := g.AcquireExec("github-2"); err != nil { // par utilisateur
		t.Errorf("un autre utilisateur est limité : %v", err)
	} else {
		r()
	}
	rel[0]() // fermeture d'un terminal : le compteur est libéré
	r, err := g.AcquireExec("github-1")
	if err != nil {
		t.Errorf("après libération : %v", err)
	} else {
		r()
	}
	rel[0]() // double libération sans effet
	n := 0
	for i := 0; i < 5; i++ {
		if r, err := g.AcquireExec("github-1"); err == nil {
			n++
			defer r()
		}
	}
	if n > 1 { // 39 pris + 1 libéré une seule fois
		t.Errorf("double libération : %d acquisitions de trop", n)
	}
}

func TestD5Plafond4Flux(t *testing.T) {
	g := NewUserGate()
	for i := 0; i < 4; i++ {
		if _, err := g.AcquireStream("github-1"); err != nil {
			t.Fatalf("flux %d/4 refusé : %v", i+1, err)
		}
	}
	if _, err := g.AcquireStream("github-1"); err != ErrGateStreams {
		t.Errorf("5e flux : %v (ErrGateStreams attendu)", err)
	}
	if _, err := g.AcquireStream("github-2"); err != nil {
		t.Errorf("un autre utilisateur est limité : %v", err)
	}
}
