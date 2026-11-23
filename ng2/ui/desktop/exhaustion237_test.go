package desktop

// #237 (v6.8.1) : la date de renouvellement du panneau de consommation suit la marge par fenêtre —
// orange (yellow) jusqu'à 1 h après le renouvellement pour la session, 5 h pour les hebdomadaires ;
// vert au-delà ; rouge si épuisé avant (alerte « ⚠ épuisé »). Complète exhaustion235_test.go.

import (
	"strings"
	"testing"
	"time"

	"claude-launcher-ng2/core"
)

func TestResetLevelMargeParFenetre237(t *testing.T) {
	sandbox(t, "", "alpha")
	var r recorder
	a := newApp(t, r.emit)
	now := time.Now()
	// 50 % consommés après `el` : épuisement projeté E = maintenant + el ; renouvellement R = maintenant + (fenêtre − el).
	row := func(kind string, win, el time.Duration) core.Limit {
		return core.Limit{Kind: kind, Percent: 50, ResetsAt: now.Add(win - el).UTC().Format(time.RFC3339Nano)}
	}
	const sess, week = 5 * time.Hour, 7 * 24 * time.Hour
	cases := []struct {
		name  string
		l     core.Limit
		level string
	}{
		{"session R − 1 h", row("five_hour", sess, 2*time.Hour), "red"},
		{"session R + 30 min", row("five_hour", sess, 2*time.Hour+45*time.Minute), "yellow"},
		{"session R + 2 h (orange en v6.8.0)", row("five_hour", sess, 3*time.Hour+30*time.Minute), "green"},
		{"hebdo R + 4 h", row("seven_day", week, 86*time.Hour), "yellow"},
		{"hebdo R + 6 h", row("seven_day", week, 87*time.Hour), "green"},
		{"hebdo R − 2 h", row("seven_day", week, 83*time.Hour), "red"},
	}
	for _, c := range cases {
		a.mu.Lock()
		a.usage = core.Usage{Limits: []core.Limit{c.l}, ReadAt: time.Now()}
		a.mu.Unlock()
		s := a.Snapshot()
		if len(s.Usage) != 1 {
			t.Fatalf("%s : %d lignes de consommation", c.name, len(s.Usage))
		}
		got := s.Usage[0]
		if got.ResetLevel != c.level {
			t.Errorf("%s : resetLevel = %q, attendu %q", c.name, got.ResetLevel, c.level)
		}
		if (c.level == "red") != strings.HasPrefix(got.Alert, "⚠ épuisé") {
			t.Errorf("%s : alerte %q (rouge seulement)", c.name, got.Alert)
		}
	}
}
