package core

import (
	"strings"
	"time"
)

// Projection linéaire de l'épuisement d'un quota (#235, marges par fenêtre #237).
// Le launcher est la seule source du calcul : calculée une seule fois ici ; les
// interfaces et usage-levels.json (contracts/usage-levels.md) n'affichent que le
// niveau (Exhaustion.Level) et le texte d'alerte.

// Niveaux de la date de renouvellement.
const (
	ExhaustRed    = "red"    // épuisé avant le renouvellement
	ExhaustYellow = "yellow" // épuisé dans la marge de la fenêtre après (session 1 h, hebdomadaire 5 h)
	ExhaustGreen  = "green"  // épuisé bien au-delà
)

// exhaustMargin : marge de l'orange selon la durée de la fenêtre (#237) :
// 5 h → 1 h, 7 j → 5 h.
func exhaustMargin(win time.Duration) time.Duration {
	if win <= 5*time.Hour {
		return time.Hour
	}
	return 5 * time.Hour
}

// UsageWindow : durée de la fenêtre d'après le kind (session 5 h, hebdo 7 j) ; 0 si inconnue (dépense…).
func UsageWindow(kind string) time.Duration {
	k := strings.ToLower(kind)
	switch {
	case strings.Contains(k, "five_hour") || strings.Contains(k, "5h") || hasWord(k, "session"):
		return 5 * time.Hour
	case strings.Contains(k, "seven_day") || strings.Contains(k, "7d") || strings.Contains(k, "week"):
		return 7 * 24 * time.Hour
	}
	return 0
}

// hasWord : « session » non précédé d'une lettre (/(^|[^a-z])session/).
func hasWord(k, w string) bool {
	for i := 0; ; {
		j := strings.Index(k[i:], w)
		if j < 0 {
			return false
		}
		j += i
		if j == 0 || k[j-1] < 'a' || k[j-1] > 'z' {
			return true
		}
		i = j + 1
	}
}

// Exhaustion : projection d'une fenêtre. Level vide = projection non fiable.
type Exhaustion struct {
	Level    string    // red, yellow, green, ou "" (non fiable)
	Before   bool      // épuisé avant le renouvellement
	Exhausts time.Time // instant d'épuisement projeté
}

// ProjectExhaustion : rythme moyen depuis le début de la fenêtre (resetsAt − durée),
// extrapolé jusqu'à 100 %. Non fiable (zéro) si fenêtre inconnue, resetsAt invalide
// ou passé, conso nulle, ou fenêtre entamée depuis moins de max(10 min, 5 %).
func ProjectExhaustion(kind string, percent float64, resetsAt string, now time.Time) Exhaustion {
	win := UsageWindow(kind)
	reset, err := time.Parse(time.RFC3339Nano, resetsAt)
	if win == 0 || err != nil || !reset.After(now) {
		return Exhaustion{}
	}
	lvl := func(ex time.Time) Exhaustion {
		switch {
		case ex.Before(reset):
			return Exhaustion{Level: ExhaustRed, Before: true, Exhausts: ex}
		case ex.Before(reset.Add(exhaustMargin(win))):
			return Exhaustion{Level: ExhaustYellow, Exhausts: ex}
		}
		return Exhaustion{Level: ExhaustGreen, Exhausts: ex}
	}
	if percent >= 100 {
		return Exhaustion{Level: ExhaustRed, Before: true, Exhausts: now}
	}
	elapsed := win - reset.Sub(now)
	minElapsed := max(10*time.Minute, time.Duration(float64(win)*0.05))
	if percent <= 0 || elapsed < minElapsed {
		return Exhaustion{}
	}
	return lvl(now.Add(time.Duration((100 - percent) / percent * float64(elapsed))))
}

// Alert : « ⚠ épuisé ~HH:MM » si épuisé avant le renouvellement, sinon "".
func (e Exhaustion) Alert(now time.Time) string {
	if !e.Before {
		return ""
	}
	when := ""
	if e.Exhausts.After(now) {
		when = " ~" + FormatReset(e.Exhausts.UTC().Format(time.RFC3339Nano))
	}
	return "⚠ épuisé" + when
}

// Exhaustion : projection de la fenêtre (cf. ProjectExhaustion).
func (l Limit) Exhaustion(now time.Time) Exhaustion {
	return ProjectExhaustion(l.Kind, l.Percent, l.ResetsAt, now)
}
