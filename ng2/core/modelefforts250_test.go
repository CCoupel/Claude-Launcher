package core

import (
	"reflect"
	"strings"
	"testing"
)

// #250 (LOT-1) — table de compatibilité modèle → efforts (P2, P6 = A), contracts/desktop-dispatch.md « v6.9.0 ».
// Helpers : companion250_test.go (c250*).

var all5Efforts250 = []string{"low", "medium", "high", "xhigh", "max"}

func TestModelEffortsTable250(t *testing.T) {
	cases := []struct {
		model string
		want  []string
		known bool
	}{
		{"opus", all5Efforts250, true},
		{"sonnet", all5Efforts250, true},
		{"haiku", all5Efforts250, true},
		{"fable", all5Efforts250, true},
		{"claude-opus-4-6", []string{"low", "medium", "high", "max"}, true},
		{"claude-sonnet-4-6", []string{"low", "medium", "high", "max"}, true},
		{"claude-opus-4-5", []string{"low", "medium", "high"}, true},
		{"claude-haiku-4-5", nil, true},
		{"claude-sonnet-4-5", nil, true},
		{"claude-sonnet-4", nil, true},             // « et plus anciens » : aucun effort
		{"claude-opus-4-1", nil, true},             // idem
		{"claude-opus-4-7", all5Efforts250, false}, // génération ≥ 4.7 : tous, non connu
		{"claude-fable-1", all5Efforts250, false},  // modèle inconnu : tous
		{"modele-inconnu", all5Efforts250, false},
		{"", all5Efforts250, false},
	}
	for _, c := range cases {
		got, known := ModelEfforts(c.model)
		if len(got) == 0 && len(c.want) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ModelEfforts(%q) = %v, attendu %v", c.model, got, c.want)
		}
		if known != c.known {
			t.Errorf("ModelEfforts(%q) known = %v, attendu %v", c.model, known, c.known)
		}
	}
	// ordre croissant (le menu l'affiche tel quel) et pas de doublon
	for _, m := range []string{"opus", "claude-opus-4-6", "claude-opus-4-5"} {
		got, _ := ModelEfforts(m)
		seen := map[string]bool{}
		for _, e := range got {
			if seen[e] {
				t.Errorf("%s : effort %q en double", m, e)
			}
			seen[e] = true
		}
	}
}

// Un effort explicite incompatible est refusé par le serveur ; « default » ne l'est jamais.
// Cas atteignable : le compagnon porte déjà un identifiant complet (R6), les menus n'en proposent pas.
func TestSetOverrideEffortIncompatibleRefuse250(t *testing.T) {
	cases := []struct {
		model, effort string
		refused       bool
	}{
		{"claude-opus-4-5", "high", false},
		{"claude-opus-4-5", "xhigh", true},
		{"claude-opus-4-5", "max", true},
		{"claude-opus-4-6", "xhigh", true},
		{"claude-opus-4-6", "max", false},
		{"claude-haiku-4-5", "low", true},
		{"claude-sonnet-4-5", "high", true},
		{"claude-opus-4-7", "xhigh", false}, // inconnu : tous acceptés
	}
	for _, c := range cases {
		proj := c250Proj(t)
		c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: "+c.model+"\n---\ncorps\n")
		before := snapshot250(t, proj)
		_, err := c250Set(t, proj, "dev-backend", nil, s250(c.effort))
		if c.refused {
			if err == nil {
				t.Errorf("%s + effort %s : refus attendu", c.model, c.effort)
			} else if !strings.Contains(err.Error(), "non pris en charge") {
				t.Errorf("%s + effort %s : message %q, attendu « effort … non pris en charge par … »", c.model, c.effort, err)
			}
			if d := diff250(before, snapshot250(t, proj)); d != "" {
				t.Errorf("%s + effort %s refusé mais écrit : %s", c.model, c.effort, d)
			}
		} else if err != nil {
			t.Errorf("%s + effort %s : refus inattendu : %v", c.model, c.effort, err)
		}
		// default n'est jamais refusé par la table
		if _, err := c250Set(t, proj, "dev-backend", nil, s250("default")); err != nil {
			t.Errorf("%s + effort default : refus inattendu : %v", c.model, err)
		}
	}
}

// Modèle seul changé, effort courant devenu incompatible → effortAdjusted : effort du template s'il est accepté.
func TestSetOverrideEffortAjusteVersTemplate250(t *testing.T) {
	proj := c250ProjTpl(t, "claude-opus-4-5", "medium") // le template fixe un identifiant complet
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: opus\neffort: xhigh\n---\ncorps\n")
	res := c250MustSet(t, proj, "dev-backend", s250("default"), nil) // → claude-opus-4-5 : xhigh refusé
	if res["effortAdjusted"] != true {
		t.Errorf("effortAdjusted : %v, attendu true", res["effortAdjusted"])
	}
	if res["model"] != "claude-opus-4-5" || res["effort"] != "medium" || res["effortSrc"] != "template" {
		t.Errorf("résultat : %v, attendu model claude-opus-4-5, effort medium (template)", res)
	}
	if got := c250Read(t, c250Companion(proj, "dev-backend")); got != "corps\n" {
		t.Errorf("compagnon : %q, attendu clés retirées (effort = template), corps seul", got)
	}
	if n, _ := res["note"].(string); !strings.Contains(strings.ToLower(n), "effort") {
		t.Errorf("la note doit dire que l'effort a été ajusté : %q", n)
	}
}

// Effort du template lui-même incompatible → le plus haut effort accepté.
func TestSetOverrideEffortAjusteVersPlusHaut250(t *testing.T) {
	proj := c250ProjTpl(t, "claude-opus-4-5", "xhigh") // template : modèle qui n'accepte pas son propre effort
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: opus\neffort: max\n---\ncorps\n")
	res := c250MustSet(t, proj, "dev-backend", s250("default"), nil)
	if res["effortAdjusted"] != true || res["effort"] != "high" || res["effortSrc"] != "companion" {
		t.Errorf("résultat : %v, attendu effort high (le plus haut accepté par claude-opus-4-5), companion, effortAdjusted", res)
	}
	if _, e := frontKeys(c250Companion(proj, "dev-backend")); e != "high" {
		t.Errorf("effort écrit : %q, attendu high", e)
	}
}

// Aucun effort accepté → clé effort retirée.
func TestSetOverrideEffortAjusteVersRetrait250(t *testing.T) {
	proj := c250ProjTpl(t, "claude-haiku-4-5", "medium")
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: opus\neffort: high\n---\ncorps\n")
	res := c250MustSet(t, proj, "dev-backend", s250("default"), nil)
	if res["effortAdjusted"] != true {
		t.Errorf("effortAdjusted : %v, attendu true", res["effortAdjusted"])
	}
	if _, e := frontKeys(c250Companion(proj, "dev-backend")); e != "" {
		t.Errorf("effort %q, attendu clé retirée (aucun effort accepté par claude-haiku-4-5)", e)
	}
}

// Effort compatible conservé : pas d'ajustement.
func TestSetOverrideEffortCompatibleNonAjuste250(t *testing.T) {
	proj := c250Proj(t)
	c250Write(t, c250Companion(proj, "dev-backend"), "---\nmodel: haiku\neffort: max\n---\ncorps\n")
	res := c250MustSet(t, proj, "dev-backend", s250("opus"), nil)
	if res["effortAdjusted"] == true {
		t.Errorf("effortAdjusted pour un effort accepté : %v", res)
	}
	if _, e := frontKeys(c250Companion(proj, "dev-backend")); e != "max" {
		t.Errorf("effort %q, attendu max conservé", e)
	}
}
