package tmux

import "testing"

// #222 (LOT-1 v6.7.0) — coque : le repli « leader en haut » ne se dit qu'une fois par fenêtre et par cause ;
// remise à zéro quand « leader à gauche » réussit ou n'est plus demandé.
func TestShellFallbackOncePerWindowAndCause222(t *testing.T) {
	const w1, w2 = "@222a", "@222b"
	noteFallback(w1, false)
	noteFallback(w2, false)
	if !noteFallback(w1, true) {
		t.Fatal("premier repli de la fenêtre : message attendu")
	}
	for i := 0; i < 5; i++ {
		if noteFallback(w1, true) {
			t.Fatalf("remise en page %d : même cause redite", i)
		}
	}
	if !noteFallback(w2, true) {
		t.Error("autre fenêtre : son premier repli doit être dit")
	}
	if noteFallback(w2, true) {
		t.Error("autre fenêtre : redite")
	}
	// retour à la mise en page normale : remise à zéro, pas de message, nouvelle cause redite une fois
	if noteFallback(w1, false) {
		t.Error("retour à la normale : aucun message attendu")
	}
	if !noteFallback(w1, true) {
		t.Error("nouvelle cause après remise à zéro : message attendu")
	}
	if noteFallback(w1, true) {
		t.Error("nouvelle cause redite")
	}
	noteFallback(w1, false)
	noteFallback(w2, false)
}

// Sans repli, jamais de message.
func TestShellNoFallbackNoMessage222(t *testing.T) {
	for i := 0; i < 3; i++ {
		if noteFallback("@222c", false) {
			t.Fatal("message sans repli")
		}
	}
}
