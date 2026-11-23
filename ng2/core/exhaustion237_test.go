package core

// Tests de spécification #237 (v6.8.1) — contracts/usage-levels.md « Calcul » : la marge de l'orange dépend de la
// durée de la fenêtre (session 5 h → 1 h, hebdomadaires 7 j → 5 h) ; borne exacte → vert ; rouge / non fiable inchangés.

import (
	"testing"
	"time"
)

// proj237 : projection d'une fenêtre dont la moitié de la conso est atteinte après `elapsed` — rythme constant, donc
// épuisement projeté E = now + elapsed (50 %) ; le renouvellement R = now + (fenêtre − elapsed), d'où E − R = 2·elapsed − fenêtre.
func proj237(kind string, win, elapsed time.Duration) Exhaustion {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	reset := now.Add(win - elapsed).Format(time.RFC3339Nano)
	return ProjectExhaustion(kind, 50, reset, now)
}

func TestMargeParFenetre237(t *testing.T) {
	t.Parallel()
	const sess, week = 5 * time.Hour, 7 * 24 * time.Hour
	type c struct {
		name    string
		kind    string
		win, el time.Duration
		level   string
	}
	tests := []c{
		// session 5 h : marge 1 h (E − R = 2·el − 5 h)
		{"session : R − 1 h → rouge", "session", sess, 2 * time.Hour, ExhaustRed},
		{"session : R + 30 min → orange", "session", sess, 2*time.Hour + 45*time.Minute, ExhaustYellow},
		{"session : R + 58 min → orange", "session", sess, 2*time.Hour + 59*time.Minute, ExhaustYellow},
		{"session : R + 1 h exactement → vert (borne stricte)", "session", sess, 3 * time.Hour, ExhaustGreen},
		{"session : R + 2 h → vert (orange en v6.8.0)", "session", sess, 3*time.Hour + 30*time.Minute, ExhaustGreen},
		{"five_hour : R + 30 min → orange", "five_hour", sess, 2*time.Hour + 45*time.Minute, ExhaustYellow},
		// hebdomadaires : marge 5 h (E − R = 2·el − 168 h)
		{"weekly_all : R + 4 h → orange", "weekly_all", week, 86 * time.Hour, ExhaustYellow},
		{"weekly_all : R + 4 h 59 → orange", "weekly_all", week, 86*time.Hour + 29*time.Minute + 30*time.Second, ExhaustYellow},
		{"weekly_all : R + 5 h exactement → vert", "weekly_all", week, 86*time.Hour + 30*time.Minute, ExhaustGreen},
		{"weekly_all : R + 6 h → vert", "weekly_all", week, 87 * time.Hour, ExhaustGreen},
		{"weekly_all : R − 2 h → rouge", "weekly_all", week, 83 * time.Hour, ExhaustRed},
		{"weekly_scoped (Fable) : R + 4 h → orange", "weekly_scoped", week, 86 * time.Hour, ExhaustYellow},
		{"weekly_scoped : R + 5 h exactement → vert", "weekly_scoped", week, 86*time.Hour + 30*time.Minute, ExhaustGreen},
		{"weekly_scoped : R + 6 h → vert", "weekly_scoped", week, 87 * time.Hour, ExhaustGreen},
	}
	for _, tt := range tests {
		if e := proj237(tt.kind, tt.win, tt.el); e.Level != tt.level {
			t.Errorf("%s : level = %q, attendu %q", tt.name, e.Level, tt.level)
		}
	}
}

// Rouge et non fiable ne changent pas avec la marge.
func TestRougeEtNonFiableInchanges237(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }
	if e := ProjectExhaustion("weekly_all", 100, at(24*time.Hour), now); e.Level != ExhaustRed || !e.Before || !e.Exhausts.Equal(now) {
		t.Errorf("épuisé déjà : %+v", e)
	}
	for name, e := range map[string]Exhaustion{
		"conso nulle":          ProjectExhaustion("session", 0, at(3*time.Hour), now),
		"fenêtre < 5 %":        ProjectExhaustion("weekly_all", 50, at(7*24*time.Hour-time.Hour), now),
		"renouvellement passé": ProjectExhaustion("session", 60, at(-time.Minute), now),
		"kind inconnu":         ProjectExhaustion("spend", 50, at(3*time.Hour), now),
	} {
		if e.Level != "" {
			t.Errorf("%s : level %q, attendu non fiable", name, e.Level)
		}
	}
}
