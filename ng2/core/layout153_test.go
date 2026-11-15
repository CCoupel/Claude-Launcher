package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// #153 (LOT-5) — organisations de la fenêtre d'un projet : clés `LAYOUT_MODE` (top | left, défaut top),
// `LAYOUT_LEFT_LEADER_PCT` (20 à 80, défaut 46) et `PROJECT_LAYOUTS` de config.conf (contracts/desktop-dispatch.md,
// section LOT-5). Les noms des fonctions de core ne sont pas fixés par le contrat : tests par l'API publique
// (ValidateConfig, ReadConfModel) ; la chaîne de mise en page elle-même est vérifiée sur un vrai tmux
// (ui/tmux/layout153_test.go) et le réglage par projet côté ui/desktop.

func confProblems(t *testing.T, conf string) []ConfigProblem {
	t.Helper()
	return ValidateConfig(ConfConf, []byte(conf))
}

func problemsMentioning(ps []ConfigProblem, key string) []string {
	var out []string
	for _, p := range ps {
		if strings.Contains(p.Message, key) {
			out = append(out, p.Message)
		}
	}
	return out
}

// Valeurs valides : aucun problème sur ces clés ; valeur inconnue de LAYOUT_MODE : un problème de validation
// (le chargement retombe sur `top`).
func TestValidateLayoutKeys(t *testing.T) {
	for _, ok := range []string{"LAYOUT_MODE=top\n", "LAYOUT_MODE=left\n", "LAYOUT_MODE=\"left\"\n", "LAYOUT_LEFT_LEADER_PCT=20\n", "LAYOUT_LEFT_LEADER_PCT=80\n",
		"LAYOUT_LEFT_LEADER_PCT=46\n", "declare -A PROJECT_LAYOUTS=([\"alpha\"]=\"left\" [\"beta\"]=\"top\")\n"} {
		ps := confProblems(t, ok)
		if got := problemsMentioning(ps, "LAYOUT_"); len(got) != 0 {
			t.Errorf("%q : problème inattendu %v", ok, got)
		}
		if got := problemsMentioning(ps, "PROJECT_LAYOUTS"); len(got) != 0 {
			t.Errorf("%q : problème inattendu %v", ok, got)
		}
	}
	if ps := confProblems(t, "LAYOUT_MODE=diagonal\n"); len(problemsMentioning(ps, "LAYOUT_MODE")) == 0 {
		t.Errorf("LAYOUT_MODE=diagonal : problème de validation attendu, obtenu %v", ps)
	}
	for _, bad := range []string{"LAYOUT_LEFT_LEADER_PCT=19\n", "LAYOUT_LEFT_LEADER_PCT=81\n", "LAYOUT_LEFT_LEADER_PCT=abc\n", "LAYOUT_LEFT_LEADER_PCT=0\n"} {
		if ps := confProblems(t, bad); len(problemsMentioning(ps, "LAYOUT_LEFT_LEADER_PCT")) == 0 {
			t.Errorf("%q : problème de validation attendu (20 à 80), obtenu %v", bad, ps)
		}
	}
	if ps := confProblems(t, "declare -A PROJECT_LAYOUTS=([\"alpha\"]=\"diagonal\")\n"); len(problemsMentioning(ps, "PROJECT_LAYOUTS")) == 0 {
		t.Errorf("PROJECT_LAYOUTS[alpha]=diagonal : problème attendu, obtenu %v", ps)
	}
}

// Modèle `kind: "config.conf"` : LAYOUT_MODE est un `enum` (top, left) du groupe de la mise en page ;
// LAYOUT_LEFT_LEADER_PCT y figure.
func TestConfModelHasLayoutSettings(t *testing.T) {
	m := ReadConfModel("LAYOUT_MODE=left\nLAYOUT_LEFT_LEADER_PCT=40\n")
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Settings []map[string]any `json:"settings"`
	}
	_ = json.Unmarshal(b, &v)
	byKey := map[string]map[string]any{}
	for _, s := range v.Settings {
		if k, _ := s["key"].(string); k != "" {
			byKey[k] = s
		}
	}
	mode, pct := byKey["LAYOUT_MODE"], byKey["LAYOUT_LEFT_LEADER_PCT"]
	if mode == nil || pct == nil {
		t.Fatalf("modèle sans LAYOUT_MODE / LAYOUT_LEFT_LEADER_PCT : %s", b)
	}
	if mode["type"] != "enum" || !strings.Contains(string(mustJSON(mode)), "left") || !strings.Contains(string(mustJSON(mode)), "top") {
		t.Errorf("LAYOUT_MODE : %v, attendu un enum top | left", mode)
	}
	if mode["value"] != "left" && fmt.Sprint(mode["value"]) != "left" {
		t.Errorf("LAYOUT_MODE value = %v, attendu left", mode["value"])
	}
	if fmt.Sprint(pct["value"]) != "40" {
		t.Errorf("LAYOUT_LEFT_LEADER_PCT value = %v, attendu 40", pct["value"])
	}
	if mode["group"] != pct["group"] && pct["group"] == nil {
		t.Errorf("groupes : %v / %v", mode["group"], pct["group"])
	}
	// défauts (clé absente) : top et 46, jamais vides
	d := ReadConfModel("")
	db, _ := json.Marshal(d)
	if !strings.Contains(string(db), "LAYOUT_MODE") || !strings.Contains(string(db), "LAYOUT_LEFT_LEADER_PCT") {
		t.Errorf("modèle d'un fichier vide sans les clés de mise en page : %s", db)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
