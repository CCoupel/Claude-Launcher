package desktop

import (
	"strings"
	"testing"
	"time"
)

// #222 (LOT-1 v6.7.0) — le repli « leader en haut » ne se dit qu'une fois par projet et par cause ; le message de pied
// est effacé au retour de la mise en page normale. Contrat : contracts/desktop-dispatch.md § « v6.7.0 ».

func footerMsg(a *App) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.msg
}

func setFooter(a *App, m string) {
	a.mu.Lock()
	a.msg = m
	a.mu.Unlock()
}

func waitFooter(t *testing.T, a *App, what string, ok func(string) bool) {
	t.Helper()
	waitUntil(t, what, 3*time.Second, func() bool { return ok(footerMsg(a)) })
}

// Une même cause (tmux refuse « leader à gauche ») rappelée à chaque remise en page : un seul message.
func TestFallbackMessageOncePerCause222(t *testing.T) {
	f := openFakeProject(t)
	tm := f.a.term(f.path)
	setFooter(f.a, "")
	tm.noteFallback(true)
	waitFooter(t, f.a, "message de repli", func(m string) bool { return strings.Contains(m, "leader à gauche refusé par tmux") })
	first := footerMsg(f.a)
	if !strings.Contains(first, tm.name) || !strings.Contains(first, "« leader en haut » appliquée") {
		t.Errorf("message = %q, attendu le nom du projet et l'organisation appliquée", first)
	}
	// le pied est vidé (autre message passé) : les remises en page suivantes, même cause, ne le redisent pas
	setFooter(f.a, "")
	for i := 0; i < 5; i++ {
		tm.noteFallback(true)
	}
	time.Sleep(200 * time.Millisecond) // setMsgSoon peut passer par une tâche de fond
	if m := footerMsg(f.a); m != "" {
		t.Errorf("message répété à la remise en page suivante : %q", m)
	}
}

// Retour à la mise en page normale : le message disparaît s'il est encore affiché, un message étranger est laissé ;
// une nouvelle cause après remise à zéro est de nouveau dite (une fois).
func TestFallbackMessageClearedOnRecovery222(t *testing.T) {
	f := openFakeProject(t)
	tm := f.a.term(f.path)
	setFooter(f.a, "")
	tm.noteFallback(true)
	waitFooter(t, f.a, "message de repli", func(m string) bool { return m != "" })
	tm.noteFallback(false) // « leader à gauche » réussit (ou le projet ne demande plus left)
	waitFooter(t, f.a, "message effacé au retour de la mise en page normale", func(m string) bool { return m == "" })

	// message étranger au repli : jamais effacé par le retour à la normale
	tm.noteFallback(true)
	waitFooter(t, f.a, "second repli (nouvelle cause)", func(m string) bool { return strings.Contains(m, "leader à gauche refusé") })
	setFooter(f.a, "✓ autre message")
	tm.noteFallback(false)
	time.Sleep(200 * time.Millisecond)
	if m := footerMsg(f.a); m != "✓ autre message" {
		t.Errorf("message étranger écrasé : %q", m)
	}
	// et la nouvelle cause est bien redite après remise à zéro
	setFooter(f.a, "")
	tm.noteFallback(true)
	waitFooter(t, f.a, "repli redit après remise à zéro", func(m string) bool { return strings.Contains(m, "leader à gauche refusé") })
}

// Sans repli, aucun message ni effacement parasite.
func TestNoFallbackNoMessage222(t *testing.T) {
	f := openFakeProject(t)
	tm := f.a.term(f.path)
	setFooter(f.a, "✓ en cours")
	for i := 0; i < 3; i++ {
		tm.noteFallback(false)
	}
	time.Sleep(150 * time.Millisecond)
	if m := footerMsg(f.a); m != "✓ en cours" {
		t.Errorf("message modifié sans repli : %q", m)
	}
}
