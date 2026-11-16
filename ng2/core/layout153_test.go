package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
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

// ── Repli sur « top » (R-F) : ArrangeTeamChecked ──────────────────────────

// layoutTarget : Target factice qui consigne les commandes ; select-layout échoue selon failOn (chaînes refusées).
type layoutTarget struct {
	calls  [][]string
	failOn func(layout string) bool
}

func (f *layoutTarget) Run(args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(args) >= 4 && args[0] == "select-layout" && f.failOn != nil && f.failOn(args[len(args)-1]) {
		return fmt.Errorf("tmux : mise en page invalide")
	}
	return nil
}
func (f *layoutTarget) Output(args ...string) ([]byte, error) { return nil, nil }

func (f *layoutTarget) layouts() []string {
	var out []string
	for _, c := range f.calls {
		if len(c) >= 4 && c[0] == "select-layout" {
			out = append(out, c[len(c)-1])
		}
	}
	return out
}

func layoutPanes(n int) (panes []WinPane, top, bot []string, topIdx, botIdx []int) {
	for i := 0; i <= n; i++ {
		panes = append(panes, WinPane{ID: fmt.Sprintf("%%%d", i), Index: i})
	}
	top = []string{"%0"}
	topIdx = []int{0}
	for i := 1; i <= n; i++ {
		bot = append(bot, fmt.Sprintf("%%%d", i))
		botIdx = append(botIdx, i)
	}
	return
}

func layoutCfg(mode string) Config {
	return Config{LayoutMode: mode, LayoutLeftLeaderPct: 46, Layout2RowLeaderPct: 50, Layout3RowLeaderNum: 3, Layout3RowMidNum: 2,
		Layout3RowDen: 7, ProjectColors: map[string]string{}, ProjectLayouts: map[string]string{}}
}

// tmux refuse la chaîne « leader à gauche » : la mise en page « leader en haut » est appliquée à la place,
// fellBack est vrai (l'appelant rend alors le message de repli : pied du navigateur / message de l'interface).
func TestArrangeTeamCheckedFallsBackToTop(t *testing.T) {
	for _, n := range []int{1, 3, 5, 8} {
		panes, top, bot, topIdx, botIdx := layoutPanes(n)
		cfgLeft, cfgTop := layoutCfg(LayoutLeft), layoutCfg(LayoutTop)
		leftChain := LayoutString(cfgLeft, 200, 50, topIdx, botIdx)
		topChain := LayoutString(cfgTop, 200, 50, topIdx, botIdx)
		if leftChain == topChain {
			t.Fatalf("n=%d : précondition, chaînes left et top identiques", n)
		}
		f := &layoutTarget{failOn: func(l string) bool { return l == leftChain }}
		if !ArrangeTeamChecked(cfgLeft, f, "@1", panes, 200, 50, top, bot, "17") {
			t.Errorf("n=%d : fellBack faux alors que la chaîne left a été refusée", n)
		}
		got := f.layouts()
		if len(got) != 2 || got[0] != leftChain || got[1] != topChain {
			t.Errorf("n=%d : select-layout = %v, attendu la chaîne left refusée puis la chaîne top %q", n, got, topChain)
		}
	}
}

// Chaîne « left » acceptée : une seule mise en page, pas de repli ; « top » : jamais de repli.
func TestArrangeTeamCheckedNoFallbackWhenAccepted(t *testing.T) {
	panes, top, bot, topIdx, botIdx := layoutPanes(4)
	for _, c := range []struct {
		mode string
	}{{LayoutLeft}, {LayoutTop}} {
		cfg := layoutCfg(c.mode)
		f := &layoutTarget{}
		if ArrangeTeamChecked(cfg, f, "@1", panes, 200, 50, top, bot, "17") {
			t.Errorf("%s : fellBack vrai alors que tmux a accepté la chaîne", c.mode)
		}
		if got := f.layouts(); len(got) != 1 || got[0] != LayoutString(cfg, 200, 50, topIdx, botIdx) {
			t.Errorf("%s : select-layout = %v, attendu la seule chaîne de l'organisation", c.mode, got)
		}
	}
}

// Un échec sur « top » lui-même ne boucle pas : « top » refusé = une tentative, sans repli ; « left » refusé puis
// « top » refusé = deux tentatives au plus.
func TestArrangeTeamCheckedFailureOnTopDoesNotLoop(t *testing.T) {
	panes, top, bot, _, _ := layoutPanes(3)
	f := &layoutTarget{failOn: func(string) bool { return true }}
	if ArrangeTeamChecked(layoutCfg(LayoutTop), f, "@1", panes, 200, 50, top, bot, "17") {
		t.Error("top refusé : fellBack vrai (il n'y a pas de repli au-dessous de top)")
	}
	if got := f.layouts(); len(got) != 1 {
		t.Errorf("top refusé : %d tentatives %v, attendu 1 (pas de boucle)", len(got), got)
	}
	f = &layoutTarget{failOn: func(string) bool { return true }}
	done := make(chan bool, 1)
	go func() { done <- ArrangeTeamChecked(layoutCfg(LayoutLeft), f, "@1", panes, 200, 50, top, bot, "17") }()
	select {
	case fb := <-done:
		if !fb {
			t.Error("left refusé : fellBack attendu vrai (le repli a été tenté)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("boucle : ArrangeTeamChecked ne rend pas la main quand tmux refuse tout")
	}
	if got := f.layouts(); len(got) != 2 {
		t.Errorf("left puis top refusés : %d tentatives %v, attendu 2", len(got), got)
	}
}

// Sans teammate, left et top donnent la même chaîne (le leader occupe toute la fenêtre) : si tmux la refuse,
// seule l'organisation demandée compte — pas de boucle non plus.
func TestArrangeTeamCheckedLeaderAloneNoLoop(t *testing.T) {
	panes, top, bot, _, _ := layoutPanes(0)
	f := &layoutTarget{failOn: func(string) bool { return true }}
	done := make(chan struct{})
	go func() {
		ArrangeTeamChecked(layoutCfg(LayoutLeft), f, "@1", panes, 200, 50, top, bot, "17")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("boucle avec le leader seul")
	}
	if got := f.layouts(); len(got) > 2 {
		t.Errorf("%d tentatives avec le leader seul : %v", len(got), got)
	}
}
