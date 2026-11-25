package core

// Tests de spécification #268 (v7.0.0) — contracts/usage-levels.md « Calcul » : projection sans délai d'attente,
// garde-fous techniques seulement, échantillon court = approximatif (Exhaustion.Approx, aucune couleur, jamais de rouge).
// Écrits avant le code : RED attendu tant que ProjectExhaustion garde son seuil max(10 min, 5 %) et n'a pas de champ Approx.

import (
	"testing"
	"time"
)

var now268 = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

// at268 : projection d'une fenêtre de `win` dont `elapsed` s'est écoulé et qui a atteint p %.
func at268(kind string, win, elapsed time.Duration, p float64) (Exhaustion, time.Time) {
	reset := now268.Add(win - elapsed)
	return ProjectExhaustion(kind, p, reset.Format(time.RFC3339Nano), now268), reset
}

const (
	sess268 = 5 * time.Hour
	week268 = 7 * 24 * time.Hour
)

func near268(t *testing.T, name string, got, want time.Time, tol time.Duration) {
	t.Helper()
	if d := got.Sub(want); d < -tol || d > tol {
		t.Errorf("%s : %s, attendu %s (± %s)", name, got.Format(time.RFC3339), want.Format(time.RFC3339), tol)
	}
}

// Session 11 % à 24 min : approximatif, exhausts_at ≈ +3 h 14, avant le renouvellement mais AUCUN rouge ni alerte.
func TestExhaustion268SessionCourteApproximative(t *testing.T) {
	t.Parallel()
	e, reset := at268("session", sess268, 24*time.Minute, 11)
	if !e.Approx {
		t.Fatalf("Approx = false : %+v", e)
	}
	if e.Level != "" || e.Before {
		t.Errorf("échantillon court : level %q before %v, attendu aucune couleur", e.Level, e.Before)
	}
	near268(t, "exhausts", e.Exhausts, now268.Add(3*time.Hour+14*time.Minute), time.Minute)
	if !e.Exhausts.Before(reset) {
		t.Errorf("l'estimation devrait tomber avant le renouvellement : %s / %s", e.Exhausts, reset)
	}
	if a := e.Alert(now268); a != "" {
		t.Errorf("alerte %q sur une estimation approximative", a)
	}
}

// Session 10 % à 90 min : calculé (≥ 30 min, ≥ 3 %), +13 h 30 > R + 1 h → vert.
func TestExhaustion268SessionLongueVerte(t *testing.T) {
	t.Parallel()
	e, _ := at268("session", sess268, 90*time.Minute, 10)
	if e.Approx || e.Level != ExhaustGreen || e.Before {
		t.Fatalf("attendu vert non approximatif : %+v", e)
	}
	near268(t, "exhausts", e.Exhausts, now268.Add(13*time.Hour+30*time.Minute), time.Minute)
}

// Hebdomadaire 10 % à 5 h 23 : calculé, ≈ +2 j 0 h 27 < R → rouge (sans l'ancien délai de 8 h 24).
func TestExhaustion268HebdoRougeDes5h23(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"weekly_all", "weekly_scoped"} {
		e, _ := at268(kind, week268, 5*time.Hour+23*time.Minute, 10)
		if e.Approx || e.Level != ExhaustRed || !e.Before {
			t.Fatalf("%s : attendu rouge non approximatif : %+v", kind, e)
		}
		near268(t, kind+" exhausts", e.Exhausts, now268.Add(48*time.Hour+27*time.Minute), 2*time.Minute)
		if a := e.Alert(now268); a == "" {
			t.Errorf("%s : alerte attendue en rouge", kind)
		}
	}
}

// p = 2 % : approximatif quelle que soit la fenêtre (temps écoulé > 0), exhausts_at calculé, pas de couleur.
func TestExhaustion268PetitPourcentageApproximatif(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		kind    string
		win, el time.Duration
	}{
		"session 2 h":   {"session", sess268, 2 * time.Hour},
		"session 4 h":   {"session", sess268, 4 * time.Hour},
		"hebdo 2 jours": {"weekly_all", week268, 48 * time.Hour},
		"hebdo 6 jours": {"weekly_all", week268, 6 * 24 * time.Hour},
	} {
		e, _ := at268(c.kind, c.win, c.el, 2)
		if !e.Approx || e.Level != "" || e.Exhausts.IsZero() {
			t.Errorf("%s : p = 2 %% attendu approximatif avec exhausts_at : %+v", name, e)
		}
		near268(t, name+" exhausts", e.Exhausts, now268.Add(time.Duration(98.0/2.0*float64(c.el))), time.Minute)
	}
}

// Bornes : 30 min (session), 3 h (hebdo), 3 % — comparaisons strictes : la borne exacte n'est PAS approximative.
func TestExhaustion268Bornes(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		kind    string
		win, el time.Duration
		p       float64
		approx  bool
	}{
		"session 29 min 59":   {"session", sess268, 30*time.Minute - time.Second, 50, true},
		"session 30 min":      {"session", sess268, 30 * time.Minute, 50, false},
		"session 30 min 01":   {"session", sess268, 30*time.Minute + time.Second, 50, false},
		"hebdo 2 h 59 min 59": {"weekly_all", week268, 3*time.Hour - time.Second, 50, true},
		"hebdo 3 h":           {"weekly_all", week268, 3 * time.Hour, 50, false},
		"hebdo scoped 3 h":    {"weekly_scoped", week268, 3 * time.Hour, 50, false},
		"p 2,99 %":            {"session", sess268, 2 * time.Hour, 2.99, true},
		"p 3 %":               {"session", sess268, 2 * time.Hour, 3, false},
		"p 3,01 %":            {"session", sess268, 2 * time.Hour, 3.01, false},
		"hebdo p 2,99 %":      {"weekly_all", week268, 3 * 24 * time.Hour, 2.99, true},
		"hebdo p 3 %":         {"weekly_all", week268, 3 * 24 * time.Hour, 3, false},
	} {
		e, _ := at268(c.kind, c.win, c.el, c.p)
		if e.Exhausts.IsZero() {
			t.Errorf("%s : projection impossible alors que seul l'échantillon est court : %+v", name, e)
			continue
		}
		if e.Approx != c.approx {
			t.Errorf("%s : Approx = %v, attendu %v (%+v)", name, e.Approx, c.approx, e)
		}
		if c.approx && (e.Level != "" || e.Before) {
			t.Errorf("%s : approximatif mais coloré : %+v", name, e)
		}
		if !c.approx && e.Level == "" {
			t.Errorf("%s : non approximatif mais sans niveau : %+v", name, e)
		}
	}
}

// Garde-fous techniques : aucune division par zéro ; projection impossible = tout nul, Approx faux.
func TestExhaustion268GardeFous(t *testing.T) {
	t.Parallel()
	rs := func(d time.Duration) string { return now268.Add(d).Format(time.RFC3339Nano) }
	for name, e := range map[string]Exhaustion{
		"p = 0":                    ProjectExhaustion("session", 0, rs(2*time.Hour), now268),
		"p < 0":                    ProjectExhaustion("session", -5, rs(2*time.Hour), now268),
		"p < 0 hebdo":              ProjectExhaustion("weekly_all", -0.1, rs(24*time.Hour), now268),
		"temps écoulé 0":           ProjectExhaustion("session", 10, rs(sess268), now268),
		"temps écoulé < 0":         ProjectExhaustion("session", 10, rs(sess268+time.Hour), now268),
		"temps écoulé 0 hebdo":     ProjectExhaustion("weekly_all", 10, rs(week268), now268),
		"resets_at passé":          ProjectExhaustion("session", 60, rs(-time.Minute), now268),
		"resets_at = maintenant":   ProjectExhaustion("session", 60, rs(0), now268),
		"resets_at invalide":       ProjectExhaustion("session", 60, "demain", now268),
		"resets_at vide":           ProjectExhaustion("session", 60, "", now268),
		"kind inconnu":             ProjectExhaustion("spend", 50, rs(2*time.Hour), now268),
		"kind vide":                ProjectExhaustion("", 50, rs(2*time.Hour), now268),
		"p = 0 au milieu de hebdo": ProjectExhaustion("weekly_all", 0, rs(3*24*time.Hour), now268),
	} {
		if e.Level != "" || e.Approx || e.Before || !e.Exhausts.IsZero() {
			t.Errorf("%s : projection impossible attendue (tout nul), obtenu %+v", name, e)
		}
	}
}

// p ≥ 100 : déjà épuisé = rouge, exhausts_at = maintenant, jamais approximatif (même fenêtre à peine entamée).
func TestExhaustion268DejaEpuiseJamaisApproximatif(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		kind    string
		win, el time.Duration
		p       float64
	}{
		"session 100 % à 1 min": {"session", sess268, time.Minute, 100},
		"session 150 %":         {"session", sess268, 2 * time.Hour, 150},
		"hebdo 100 % à 10 min":  {"weekly_all", week268, 10 * time.Minute, 100},
		"hebdo scoped 100,5 %":  {"weekly_scoped", week268, 5 * 24 * time.Hour, 100.5},
	} {
		e, _ := at268(c.kind, c.win, c.el, c.p)
		if e.Approx || e.Level != ExhaustRed || !e.Before || !e.Exhausts.Equal(now268) {
			t.Errorf("%s : attendu rouge, exhausts_at = maintenant, non approximatif : %+v", name, e)
		}
		if a := e.Alert(now268); a != "⚠ épuisé" {
			t.Errorf("%s : alerte %q, attendu « ⚠ épuisé »", name, a)
		}
	}
}

// Non-régression des niveaux calculés : au-delà de l'échantillon court, mêmes marges qu'en v6.8.1 (1 h / 5 h).
func TestExhaustion268NiveauxCalculesInchanges(t *testing.T) {
	t.Parallel()
	// p = 50 % : E = now + elapsed ; E − R = 2·elapsed − fenêtre
	for name, c := range map[string]struct {
		kind    string
		win, el time.Duration
		want    string
	}{
		"session rouge":       {"session", sess268, 2 * time.Hour, ExhaustRed},
		"session orange":      {"session", sess268, 2*time.Hour + 45*time.Minute, ExhaustYellow},
		"session vert":        {"session", sess268, 3*time.Hour + 30*time.Minute, ExhaustGreen},
		"session borne R+1 h": {"session", sess268, 3 * time.Hour, ExhaustGreen},
		"hebdo orange":        {"weekly_all", week268, 3*24*time.Hour + 14*time.Hour, ExhaustYellow},
		"hebdo vert":          {"weekly_all", week268, 5 * 24 * time.Hour, ExhaustGreen},
		"hebdo rouge":         {"weekly_scoped", week268, 3 * 24 * time.Hour, ExhaustRed},
	} {
		e, _ := at268(c.kind, c.win, c.el, 50)
		if e.Approx || e.Level != c.want {
			t.Errorf("%s : level %q approx %v, attendu %q non approximatif", name, e.Level, e.Approx, c.want)
		}
	}
}
