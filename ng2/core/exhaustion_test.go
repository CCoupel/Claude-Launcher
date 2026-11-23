package core

import (
	"strings"
	"testing"
	"time"
)

// mêmes valeurs que lib.test.ts du panneau milestone (template 0.6.0)
func TestExhaustion235(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }
	reset := at(3 * time.Hour)
	tests := []struct {
		name, kind string
		pct        float64
		reset      string
		level      string
		before     bool
	}{
		{"rouge : épuisé dans 1h20 < renew", "five_hour", 60, reset, ExhaustRed, true},
		{"orange : 4h40 → renew + 1h40 < 6 h", "five_hour", 30, reset, ExhaustYellow, false},
		{"vert : bien au-delà", "five_hour", 5, reset, ExhaustGreen, false},
		{"non fiable : conso nulle", "five_hour", 0, reset, "", false},
		{"non fiable : fenêtre entamée < 5 %", "five_hour", 50, at(4*time.Hour + 57*time.Minute), "", false},
		{"kind inconnu (dépense)", "spend", 50, reset, "", false},
		{"épuisé déjà (hebdo)", "weekly_all", 100, at(24 * time.Hour), ExhaustRed, true},
		{"session (kind du launcher)", "session", 60, reset, ExhaustRed, true},
		{"hebdo ciblé", "weekly_scoped", 5, at(24 * time.Hour), ExhaustGreen, false},
		{"renouvellement passé", "five_hour", 60, at(-time.Minute), "", false},
		{"resetsAt vide", "five_hour", 60, "", "", false},
	}
	for _, tt := range tests {
		e := ProjectExhaustion(tt.kind, tt.pct, tt.reset, now)
		if e.Level != tt.level || e.Before != tt.before {
			t.Errorf("%s : level=%q before=%v, attendu %q %v", tt.name, e.Level, e.Before, tt.level, tt.before)
		}
	}
	e := ProjectExhaustion("five_hour", 60, reset, now)
	if d := e.Exhausts.Sub(now); d.Round(time.Minute) != 80*time.Minute {
		t.Errorf("épuisement dans %v, attendu ~80 min", d)
	}
	if a := e.Alert(now); !strings.HasPrefix(a, "⚠ épuisé ~") {
		t.Errorf("alerte rouge = %q", a)
	}
	if a := ProjectExhaustion("five_hour", 20, reset, now).Alert(now); a != "" {
		t.Errorf("pas d'alerte hors rouge, obtenu %q", a)
	}
	if a := ProjectExhaustion("weekly_all", 100, at(24*time.Hour), now).Alert(now); a != "⚠ épuisé" {
		t.Errorf("épuisé déjà : %q", a)
	}
}
